package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

// Service owns generic release lifecycle decisions. Workers may create a
// snapshot before execution, but only this service's repository transaction
// decides completion and moves generic application channels.
type Service struct {
	releases repository.ApplicationReleaseRepository
}

func NewService(releases repository.ApplicationReleaseRepository) *Service {
	return &Service{releases: releases}
}

func (s *Service) CreateSnapshot(ctx context.Context, build domain.Build) (domain.ApplicationRelease, error) {
	if build.ApplicationVersion == nil || strings.TrimSpace(*build.ApplicationVersion) == "" {
		return domain.ApplicationRelease{}, errors.New("application release requires a resolved application version")
	}
	if build.PipelineConfigYAML == nil || strings.TrimSpace(*build.PipelineConfigYAML) == "" {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	file, err := pipeline.ParseAndValidate([]byte(*build.PipelineConfigYAML))
	if err != nil {
		return domain.ApplicationRelease{}, fmt.Errorf("parse release pipeline snapshot: %w", err)
	}
	resolved := pipeline.Resolve(file)
	if resolved.Release == nil {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if build.JobID == nil && strings.TrimSpace(resolved.Release.AutoChannel) != "" {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseChannelScope
	}
	outputs, err := releaseOutputs(resolved, build)
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	version := strings.TrimSpace(*build.ApplicationVersion)
	release := domain.ApplicationRelease{
		ID: uuid.NewString(), BuildID: build.ID, ProjectID: build.ProjectID, JobID: build.JobID,
		ApplicationVersion: version, AutoChannelName: resolved.Release.AutoChannel,
		CreatedAt: time.Now().UTC(), Outputs: outputs,
	}
	created, err := s.releases.Create(ctx, release)
	if errors.Is(err, repository.ErrApplicationReleaseConflict) {
		return domain.ApplicationRelease{}, err
	}
	if err != nil {
		return domain.ApplicationRelease{}, err
	}
	return created, nil
}

func (s *Service) EnsureSnapshot(ctx context.Context, build domain.Build) error {
	_, err := s.releases.GetByBuildID(ctx, build.ID)
	if err == nil || !errors.Is(err, repository.ErrApplicationReleaseNotFound) {
		return err
	}
	_, err = s.CreateSnapshot(ctx, build)
	if errors.Is(err, repository.ErrApplicationReleaseNotFound) {
		return nil
	}
	return err
}

func (s *Service) Reconcile(ctx context.Context, releaseID string) (domain.ApplicationRelease, error) {
	return s.releases.Reconcile(ctx, releaseID)
}

func (s *Service) ReconcilePending(ctx context.Context, limit int) error {
	pending, err := s.releases.ListPending(ctx, limit)
	if err != nil {
		return err
	}
	var reconcileErrors []error
	for _, release := range pending {
		if _, reconcileErr := s.Reconcile(ctx, release.ID); reconcileErr != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile release %s: %w", release.ID, reconcileErr))
		}
	}
	return errors.Join(reconcileErrors...)
}

func (s *Service) FailBuildRelease(ctx context.Context, build domain.Build) error {
	if build.Status != domain.BuildStatusFailed && build.Status != domain.BuildStatusCanceled {
		return nil
	}
	reason := "build " + string(build.Status)
	if build.ErrorMessage != nil && strings.TrimSpace(*build.ErrorMessage) != "" {
		reason = strings.TrimSpace(*build.ErrorMessage)
	}
	_, err := s.releases.FailBuildRelease(ctx, build.ID, reason)
	if errors.Is(err, repository.ErrApplicationReleaseNotFound) {
		return nil
	}
	return err
}

func (s *Service) Get(ctx context.Context, releaseID string) (domain.ApplicationRelease, error) {
	return s.releases.GetByID(ctx, releaseID)
}

func (s *Service) GetByBuildID(ctx context.Context, buildID string) (domain.ApplicationRelease, error) {
	return s.releases.GetByBuildID(ctx, buildID)
}

func (s *Service) PromoteStable(ctx context.Context, releaseID string, actorUserID *string) (domain.ApplicationRelease, error) {
	return s.releases.PromoteStable(ctx, releaseID, actorUserID)
}

func releaseOutputs(resolved *pipeline.ResolvedPipeline, build domain.Build) ([]domain.ApplicationReleaseOutput, error) {
	if resolved.Release == nil {
		return nil, nil
	}
	artifactDeclarations := map[string]domain.ArtifactDeclaration{}
	for _, declaration := range resolved.Artifacts.Declarations {
		if name := strings.TrimSpace(declaration.Name); name != "" {
			artifactDeclarations[name] = declaration
		}
	}
	imageSteps := map[string]pipeline.ResolvedStep{}
	for _, step := range resolved.Steps {
		if step.ExecutionKind == domain.ExecutionKindImageBuild {
			if name := strings.TrimSpace(step.Name); name != "" {
				imageSteps[name] = step
			}
		}
	}
	outputs := make([]domain.ApplicationReleaseOutput, 0, len(resolved.Release.Artifacts)+len(resolved.Release.Images))
	for _, name := range resolved.Release.Artifacts {
		name = strings.TrimSpace(name)
		declaration := artifactDeclarations[name]
		definition, err := json.Marshal(map[string]string{"path": declaration.Path, "name": name})
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, domain.ApplicationReleaseOutput{ID: uuid.NewString(), Kind: domain.ApplicationReleaseOutputArtifact, OutputKey: "artifact:" + name, DisplayName: name, DefinitionJSON: string(definition), CreatedAt: time.Now().UTC()})
	}
	for _, name := range resolved.Release.Images {
		name = strings.TrimSpace(name)
		step := imageSteps[name]
		definition, err := json.Marshal(map[string]string{"node_id": step.NodeID, "name": name, "target_image_reference": step.RemoteImageBuild.TargetImageReference})
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, domain.ApplicationReleaseOutput{ID: uuid.NewString(), Kind: domain.ApplicationReleaseOutputImage, OutputKey: "image:" + releaseImageOutputIdentity(name), DisplayName: name, DefinitionJSON: string(definition), CreatedAt: time.Now().UTC()})
	}
	return outputs, nil
}

func releaseImageOutputIdentity(name string) string {
	var result strings.Builder
	separator := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(character)
			separator = false
			continue
		}
		separator = true
	}
	return result.String()
}
