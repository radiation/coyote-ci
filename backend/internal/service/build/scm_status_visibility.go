package build

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type BuildSCMStatusView struct {
	Reportable          bool
	Configured          bool
	Provider            string
	RepositoryOwner     string
	RepositoryName      string
	CommitSHA           *string
	Context             *string
	DesiredState        *string
	LastSentState       *string
	DeliveryState       *string
	CurrentOwnerBuildID *string
	CurrentOwnerAttempt *int
	Attempts            *int
	NextAttemptAt       *time.Time
	LastError           *string
	AwaitingReassertion bool
}

func (s *BuildService) GetBuildSCMStatus(ctx context.Context, build domain.Build, project *domain.Project) (*BuildSCMStatusView, error) {
	provider, owner, repo, linked := scmStatusRepositoryIdentity(build)
	if !linked {
		return nil, nil
	}

	view := &BuildSCMStatusView{
		Configured:      s.scmStatusReporter != nil,
		Provider:        provider,
		RepositoryOwner: owner,
		RepositoryName:  repo,
	}
	if commitSHA := strings.TrimSpace(scmStatusCommitSHA(build)); commitSHA != "" {
		view.CommitSHA = buildSCMStringPtr(commitSHA)
	}
	if project != nil {
		if contextName, ok := scmStatusBuildContextName(build, *project); ok {
			view.Context = buildSCMStringPtr(contextName)
		}
	}
	if desiredState, _, ok := scmStatusStateForBuild(build); ok {
		view.DesiredState = buildSCMStringPtr(string(desiredState))
	}
	view.Reportable = view.CommitSHA != nil && view.Context != nil && view.DesiredState != nil

	if s.scmStatusDeliveryRepo == nil || !view.Reportable {
		return view, nil
	}

	delivery, err := s.scmStatusDeliveryRepo.GetByKey(ctx, provider, owner, repo, *view.CommitSHA, *view.Context)
	if err != nil {
		if errors.Is(err, repository.ErrSCMStatusDeliveryNotFound) {
			return view, nil
		}
		return nil, err
	}
	if strings.TrimSpace(delivery.BuildID) == strings.TrimSpace(build.ID) && delivery.BuildAttempt == build.AttemptNumber {
		applyBuildSCMDelivery(view, delivery)
		return view, nil
	}
	applyBuildSCMSupersededDelivery(view, delivery)

	return view, nil
}

func scmStatusBuildContextName(build domain.Build, project domain.Project) (string, bool) {
	if build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
		return "", false
	}
	projectSlug := strings.TrimSpace(project.Slug)
	if projectSlug == "" {
		return "", false
	}
	return scmStatusContextName(projectSlug, strings.TrimSpace(*build.JobID)), true
}

func applyBuildSCMDelivery(view *BuildSCMStatusView, delivery domain.SCMStatusDelivery) {
	delivery = delivery.Normalize()
	view.CommitSHA = buildSCMStringPtr(delivery.CommitSHA)
	view.Context = buildSCMStringPtr(delivery.Context)
	view.DesiredState = buildSCMStringPtr(string(delivery.DesiredState))
	view.DeliveryState = buildSCMStringPtr(string(delivery.Status))
	view.Attempts = buildSCMIntPtr(delivery.Attempts)
	view.NextAttemptAt = delivery.NextAttemptAt
	view.LastError = cloneBuildSCMString(delivery.LastError)
	if delivery.LastSentState != nil {
		view.LastSentState = buildSCMStringPtr(string(*delivery.LastSentState))
	}
	view.AwaitingReassertion = delivery.Status == domain.SCMStatusDeliveryStatusRetryWaiting && strings.TrimSpace(buildSCMOptionalString(delivery.FailureReason)) == scmStatusFailureReasonAuthoritativeReassert
}

func applyBuildSCMSupersededDelivery(view *BuildSCMStatusView, delivery domain.SCMStatusDelivery) {
	delivery = delivery.Normalize()
	view.CommitSHA = buildSCMStringPtr(delivery.CommitSHA)
	view.Context = buildSCMStringPtr(delivery.Context)
	view.DesiredState = buildSCMStringPtr(string(delivery.DesiredState))
	view.DeliveryState = buildSCMStringPtr(string(domain.SCMStatusDeliveryStatusSuperseded))
	view.CurrentOwnerBuildID = buildSCMStringPtr(delivery.BuildID)
	view.CurrentOwnerAttempt = buildSCMIntPtr(delivery.BuildAttempt)
	view.LastSentState = nil
	view.Attempts = nil
	view.NextAttemptAt = nil
	view.LastError = nil
	view.AwaitingReassertion = false
}

func buildSCMStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildSCMIntPtr(value int) *int {
	if value < 0 {
		return nil
	}
	return &value
}

func cloneBuildSCMString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildSCMOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
