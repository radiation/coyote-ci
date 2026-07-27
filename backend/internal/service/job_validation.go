package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

func normalizeCreateJobInput(input CreateJobInput) (CreateJobInput, error) {
	normalized := input
	normalized.ProjectID = strings.TrimSpace(normalized.ProjectID)
	normalized.ProjectSlug = strings.TrimSpace(normalized.ProjectSlug)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.RepositoryID = strings.TrimSpace(normalized.RepositoryID)
	normalized.RepositoryURL = strings.TrimSpace(normalized.RepositoryURL)
	normalized.DefaultRef = strings.TrimSpace(normalized.DefaultRef)
	normalized.DefaultCommitSHA = strings.TrimSpace(normalized.DefaultCommitSHA)
	if normalized.PushBranch != nil {
		branch := normalizePushRef(*normalized.PushBranch)
		normalized.PushBranch = &branch
	}
	if normalized.TriggerMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*normalized.TriggerMode))
		normalized.TriggerMode = &mode
	}
	normalized.PipelineYAML = strings.TrimSpace(normalized.PipelineYAML)
	normalized.PipelinePath = strings.TrimSpace(normalized.PipelinePath)

	if err := validateCreateJobRequiredFields(normalized); err != nil {
		return CreateJobInput{}, err
	}

	normalized.BranchAllowlist = normalizeBranchAllowlist(normalized.BranchAllowlist)
	normalized.TagAllowlist = normalizeTagAllowlist(normalized.TagAllowlist)
	normalized.ArtifactTriggers = domain.NormalizeJobArtifactTriggers(normalized.ArtifactTriggers)

	return normalized, nil
}

func validateCreateJobRequiredFields(input CreateJobInput) error {
	if input.Name == "" {
		return ErrJobNameRequired
	}
	if input.RepositoryID != "" && input.RepositoryURL != "" {
		return ErrJobRepositoryAssignmentConflict
	}
	if input.RepositoryID == "" && input.RepositoryURL == "" {
		return ErrJobRepositorySourceRequired
	}
	if input.DefaultRef == "" && input.DefaultCommitSHA == "" {
		return ErrJobSourceTargetRequired
	}
	if input.PipelineYAML == "" && input.PipelinePath == "" {
		return ErrJobPipelineDefinitionRequired
	}
	if input.TriggerMode != nil {
		if !isValidTriggerMode(*input.TriggerMode) {
			return ErrJobInvalidTriggerMode
		}
	}
	if input.Priority != nil && !domain.ValidPriority(*input.Priority) {
		return ErrJobPriorityOutOfRange
	}
	if err := validateRawArtifactTriggers(input.ArtifactTriggers); err != nil {
		return err
	}

	return nil
}

func validateRawArtifactTriggers(triggers []domain.JobArtifactTrigger) error {
	for _, trigger := range triggers {
		if strings.TrimSpace(trigger.ProducerJobID) == "" {
			return ErrJobArtifactTriggerProducerJobIDRequired
		}
		if strings.TrimSpace(trigger.Path) == "" {
			return ErrJobArtifactTriggerPathRequired
		}
	}
	return nil
}

func (s *JobService) resolveProjectID(ctx context.Context, projectID string, projectSlug string) (string, error) {
	trimmedID := strings.TrimSpace(projectID)
	trimmedSlug := strings.TrimSpace(projectSlug)
	if s.projects == nil {
		if trimmedID == "" {
			return "", ErrJobProjectIDRequired
		}
		return trimmedID, nil
	}

	if trimmedID != "" {
		if _, err := uuid.Parse(trimmedID); err == nil {
			project, lookupErr := s.projects.GetByID(ctx, trimmedID)
			if lookupErr != nil {
				return "", lookupErr
			}
			return project.ID, nil
		}
		if trimmedSlug == "" {
			trimmedSlug = trimmedID
		}
	}
	if trimmedSlug != "" {
		project, err := s.projects.GetBySlug(ctx, normalizeProjectSlug(trimmedSlug))
		if err != nil {
			return "", err
		}
		return project.ID, nil
	}

	project, err := s.projects.GetBySlug(ctx, domain.DefaultProjectSlug)
	if err != nil {
		return "", err
	}
	return project.ID, nil
}

func validateJobRequiredFields(job domain.Job) error {
	repositoryID := readStringPtr(job.RepositoryID)
	repositoryURL := strings.TrimSpace(job.RepositoryURL)
	if strings.TrimSpace(job.Name) == "" {
		return ErrJobNameRequired
	}
	if repositoryID == "" && repositoryURL == "" {
		return ErrJobRepositorySourceRequired
	}
	if strings.TrimSpace(job.DefaultRef) == "" && strings.TrimSpace(readStringPtr(job.DefaultCommitSHA)) == "" {
		return ErrJobSourceTargetRequired
	}
	if strings.TrimSpace(job.PipelineYAML) == "" && strings.TrimSpace(readStringPtr(job.PipelinePath)) == "" {
		return ErrJobPipelineDefinitionRequired
	}
	if !isValidTriggerMode(string(optionalJobTriggerMode(job.TriggerMode))) {
		return ErrJobInvalidTriggerMode
	}
	if !domain.ValidPriority(job.Priority) {
		return ErrJobPriorityOutOfRange
	}
	return validateRawArtifactTriggers(domain.NormalizeJobArtifactTriggers(job.ArtifactTriggers))
}

func optionalJobTriggerMode(mode domain.JobTriggerMode) string {
	trimmed := strings.TrimSpace(string(mode))
	if trimmed == "" {
		return string(domain.JobTriggerModeBranches)
	}
	return trimmed
}

func validatePipelineDefinition(pipelineYAML string, pipelinePath *string) error {
	if strings.TrimSpace(pipelineYAML) == "" && strings.TrimSpace(readStringPtr(pipelinePath)) == "" {
		return ErrJobPipelineDefinitionRequired
	}
	return nil
}

func normalizedPriority(priority *int) int {
	if priority == nil {
		return domain.DefaultPriority
	}
	return domain.NormalizePriority(*priority)
}

func validatePipelineYAML(yamlText string) error {
	trimmed := strings.TrimSpace(yamlText)
	if trimmed == "" {
		return ErrJobPipelineDefinitionRequired
	}

	_, err := pipeline.LoadAndResolve([]byte(trimmed))
	if err != nil {
		return err
	}

	return nil
}

func readStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalTrimmedStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeBranchAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, item := range values {
		branch := normalizePushRef(item)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		normalized = append(normalized, branch)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeTagAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, item := range values {
		trimmed := strings.TrimSpace(strings.TrimPrefix(item, "refs/tags/"))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func isValidTriggerMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(domain.JobTriggerModeBranches), string(domain.JobTriggerModeTags), string(domain.JobTriggerModeBranchesAndTags):
		return true
	default:
		return false
	}
}
