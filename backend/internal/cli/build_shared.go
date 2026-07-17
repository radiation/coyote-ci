package cli

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
)

type buildStatusPayload struct {
	Build      buildStatusView      `json:"build"`
	FailedStep *buildFailedStepView `json:"failed_step,omitempty"`
}

type buildStatusView struct {
	ID           string                      `json:"id"`
	BuildNumber  int64                       `json:"build_number,omitempty"`
	ProjectID    string                      `json:"project_id"`
	ProjectName  *string                     `json:"project_name,omitempty"`
	JobID        *string                     `json:"job_id,omitempty"`
	JobName      *string                     `json:"job_name,omitempty"`
	Status       string                      `json:"status"`
	Ref          *string                     `json:"ref,omitempty"`
	SHA          *string                     `json:"sha,omitempty"`
	Author       *string                     `json:"author,omitempty"`
	CreatedAt    string                      `json:"created_at"`
	StartedAt    *string                     `json:"started_at,omitempty"`
	FinishedAt   *string                     `json:"finished_at,omitempty"`
	DurationMS   *int64                      `json:"duration_ms,omitempty"`
	WebURL       string                      `json:"web_url"`
	Error        *string                     `json:"error_message,omitempty"`
	Pipeline     *string                     `json:"pipeline_name,omitempty"`
	SCMStatus    *api.BuildSCMStatusResponse `json:"scm_status,omitempty"`
	CurrentSteps []buildCurrentStepView      `json:"current_steps"`
}

type buildCurrentStepView struct {
	ID        string  `json:"id"`
	Index     int     `json:"index"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	StartedAt *string `json:"started_at,omitempty"`
}

type buildFailedStepView struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

type buildRetryPayload struct {
	Retried buildRetryView `json:"retried"`
}

type buildRetryView struct {
	SourceBuildID string `json:"source_build_id"`
	BuildID       string `json:"build_id"`
	Status        string `json:"status"`
	WebURL        string `json:"web_url,omitempty"`
}

type buildArtifactsPayload struct {
	BuildID   string                  `json:"build_id"`
	Artifacts []buildArtifactListView `json:"artifacts"`
}

type buildArtifactTriggerDeliveriesPayload struct {
	BuildID                  string                             `json:"build_id"`
	BuildTriggerKind         string                             `json:"build_trigger_kind"`
	RecursiveDispatchBlocked bool                               `json:"recursive_dispatch_blocked"`
	Summary                  buildArtifactTriggerSummaryView    `json:"summary"`
	Deliveries               []buildArtifactTriggerDeliveryView `json:"deliveries"`
}

type buildArtifactTriggerSummaryView struct {
	DeliveryCount int `json:"delivery_count"`
	QueuedCount   int `json:"queued_count"`
	FailedCount   int `json:"failed_count"`
}

type buildArtifactTriggerDeliveryView struct {
	DeliveryID        string  `json:"delivery_id"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	ProducerBuildID   string  `json:"producer_build_id"`
	ProducerProjectID string  `json:"producer_project_id"`
	ProducerJobID     string  `json:"producer_job_id"`
	ArtifactID        string  `json:"artifact_id"`
	ArtifactPath      string  `json:"artifact_path"`
	ArtifactName      *string `json:"artifact_name,omitempty"`
	ArtifactSizeBytes *int64  `json:"artifact_size_bytes,omitempty"`
	ConsumerJobID     string  `json:"consumer_job_id"`
	ConsumerJobName   *string `json:"consumer_job_name,omitempty"`
	DownstreamBuildID *string `json:"downstream_build_id,omitempty"`
	ErrorMessage      *string `json:"error_message,omitempty"`
}

type buildArtifactTriggerRetryPayload struct {
	Result   string                           `json:"result"`
	Message  string                           `json:"message,omitempty"`
	Delivery buildArtifactTriggerDeliveryView `json:"delivery"`
}

type buildArtifactListView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	Path        string  `json:"path"`
	StepID      *string `json:"step_id,omitempty"`
	SizeBytes   int64   `json:"size_bytes"`
	ContentType *string `json:"content_type,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type buildArtifactDownloadPayload struct {
	BuildID    string                      `json:"build_id"`
	Downloaded []buildArtifactDownloadView `json:"downloaded"`
}

type buildArtifactDownloadView struct {
	ArtifactID      string  `json:"artifact_id"`
	Name            string  `json:"name"`
	ArtifactPath    string  `json:"artifact_path"`
	StepID          *string `json:"step_id,omitempty"`
	ContentType     *string `json:"content_type,omitempty"`
	SizeBytes       int64   `json:"size_bytes"`
	Path            string  `json:"path,omitempty"`
	LocalPath       string  `json:"local_path"`
	DownloadedBytes int64   `json:"downloaded_bytes"`
}

func trimStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func firstJobName(steps []api.BuildStepResponse) *string {
	for _, step := range steps {
		if step.Job == nil {
			continue
		}
		name := strings.TrimSpace(step.Job.Name)
		if name != "" {
			return &name
		}
	}
	return nil
}

func firstFailedStep(steps []api.BuildStepResponse) *buildFailedStepView {
	ordered := append([]api.BuildStepResponse(nil), steps...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].StepIndex < ordered[j].StepIndex
	})
	for _, step := range ordered {
		if step.Status == "failed" {
			return &buildFailedStepView{Index: step.StepIndex, Name: step.Name, Status: step.Status, ExitCode: step.ExitCode}
		}
	}
	return nil
}

func makeBuildCurrentStepViews(steps []api.BuildCurrentStepResponse) []buildCurrentStepView {
	if len(steps) == 0 {
		return []buildCurrentStepView{}
	}
	items := make([]buildCurrentStepView, 0, len(steps))
	for _, step := range steps {
		items = append(items, buildCurrentStepView{
			ID:        step.ID,
			Index:     step.Index,
			Name:      step.Name,
			Status:    step.Status,
			StartedAt: trimStringPtr(step.StartedAt),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Index == items[j].Index {
			return items[i].ID < items[j].ID
		}
		return items[i].Index < items[j].Index
	})
	return items
}

func firstNonEmptyPtr(values ...*string) *string {
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			return &trimmed
		}
	}
	return nil
}

func buildDurationMS(startedAt *string, finishedAt *string) *int64 {
	if startedAt == nil || finishedAt == nil {
		return nil
	}
	started, err := time.Parse(time.RFC3339, *startedAt)
	if err != nil {
		return nil
	}
	finished, err := time.Parse(time.RFC3339, *finishedAt)
	if err != nil || finished.Before(started) {
		return nil
	}
	duration := finished.Sub(started).Milliseconds()
	return &duration
}

func buildWebURL(serverURL string, buildID string, failedStep *buildFailedStepView) string {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	basePath := strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = basePath + "/builds/" + url.PathEscape(strings.TrimSpace(buildID))
	if failedStep != nil {
		query := url.Values{}
		query.Set("step", strconv.Itoa(failedStep.Index))
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

func displayProjectLabel(projectName *string, projectID string) string {
	if projectName != nil && strings.TrimSpace(*projectName) != "" {
		return *projectName
	}
	return projectID
}

func displayJobLabel(jobName *string, jobID *string) string {
	if jobName != nil && strings.TrimSpace(*jobName) != "" {
		return *jobName
	}
	if jobID != nil && strings.TrimSpace(*jobID) != "" {
		return *jobID
	}
	return "manual"
}

func shortSHA(sha string) string {
	trimmed := strings.TrimSpace(sha)
	if len(trimmed) <= 7 {
		return trimmed
	}
	return trimmed[:7]
}

func formatDurationMS(durationMS int64) string {
	return (time.Duration(durationMS) * time.Millisecond).String()
}
