package cli

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type buildStatusPayload struct {
	Build      buildStatusView      `json:"build"`
	FailedStep *buildFailedStepView `json:"failed_step,omitempty"`
}

type buildStatusView struct {
	ID          string  `json:"id"`
	BuildNumber int64   `json:"build_number,omitempty"`
	ProjectID   string  `json:"project_id"`
	ProjectName *string `json:"project_name,omitempty"`
	JobID       *string `json:"job_id,omitempty"`
	JobName     *string `json:"job_name,omitempty"`
	Status      string  `json:"status"`
	Ref         *string `json:"ref,omitempty"`
	SHA         *string `json:"sha,omitempty"`
	Author      *string `json:"author,omitempty"`
	CreatedAt   string  `json:"created_at"`
	StartedAt   *string `json:"started_at,omitempty"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	DurationMS  *int64  `json:"duration_ms,omitempty"`
	WebURL      string  `json:"web_url"`
	Error       *string `json:"error_message,omitempty"`
	Pipeline    *string `json:"pipeline_name,omitempty"`
}

type buildFailedStepView struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

func (a *app) newBuildCommand() *cobra.Command {
	command := &cobra.Command{Use: "build", Short: "Inspect builds"}
	command.AddCommand(a.newBuildStatusCommand())
	command.AddCommand(a.newBuildLogsCommand())
	return command
}

func (a *app) newBuildStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <build-id>",
		Short: "Show build status and metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			build, buildErr := client.GetBuild(cmd.Context(), args[0])
			if buildErr != nil {
				return mapCommandError(buildErr)
			}
			steps, stepsErr := client.GetBuildSteps(cmd.Context(), args[0])
			if stepsErr != nil {
				return mapCommandError(stepsErr)
			}

			payload := makeBuildStatusPayload(resolved.ServerURL, build, steps)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildStatusHuman(w, payload)
			}, payload)
		},
	}
}

func (a *app) newBuildLogsCommand() *cobra.Command {
	var stepRaw string
	var failed bool
	var tail int

	command := &cobra.Command{
		Use:   "logs <build-id>",
		Short: "Show build logs",
		Long:  "Fetch a snapshot of current build logs. Re-run the command to fetch newer logs. Live log following is deferred.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := parseBuildLogsOptions(stepRaw, failed, tail)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}

			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			logsResponse, logsErr := client.GetBuildLogs(cmd.Context(), args[0], options)
			if logsErr != nil {
				return mapCommandError(logsErr)
			}

			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildLogsHuman(w, logsResponse)
			}, logsResponse)
		},
	}
	command.Flags().StringVar(&stepRaw, "step", "", "Limit logs to one step index")
	command.Flags().BoolVar(&failed, "failed", false, "Select the failed step when exactly one step failed")
	command.Flags().IntVar(&tail, "tail", 0, "Show only the last N log entries")
	return command
}

func parseBuildLogsOptions(stepRaw string, failed bool, tail int) (apiclient.BuildLogsOptions, error) {
	var options apiclient.BuildLogsOptions
	trimmedStep := strings.TrimSpace(stepRaw)
	if trimmedStep != "" {
		stepIndex, err := strconv.Atoi(trimmedStep)
		if err != nil || stepIndex < 0 {
			return apiclient.BuildLogsOptions{}, fmt.Errorf("step must be a non-negative integer")
		}
		options.Step = &stepIndex
	}
	if options.Step != nil && failed {
		return apiclient.BuildLogsOptions{}, fmt.Errorf("step and failed cannot be used together")
	}
	if tail < 0 {
		return apiclient.BuildLogsOptions{}, fmt.Errorf("tail must be a positive integer")
	}
	options.Failed = failed
	options.Tail = tail
	return options, nil
}

func makeBuildStatusPayload(serverURL string, build api.BuildResponse, steps []api.BuildStepResponse) buildStatusPayload {
	jobName := firstJobName(steps)
	refValue := firstNonEmptyPtr(build.SourceRef, build.TriggerRef)
	shaValue := firstNonEmptyPtr(build.SourceCommitSHA, build.SourceSHA, build.TriggerCommitSHA)
	authorValue := firstNonEmptyPtr(build.SourceAuthorName, build.TriggeredBy, build.Actor)
	failedStep := firstFailedStep(steps)
	durationMS := buildDurationMS(build.StartedAt, build.FinishedAt)
	webURL := buildWebURL(serverURL, build.ID, failedStep)

	payload := buildStatusPayload{
		Build: buildStatusView{
			ID:          build.ID,
			BuildNumber: build.BuildNumber,
			ProjectID:   build.ProjectID,
			ProjectName: build.ProjectName,
			JobID:       build.JobID,
			JobName:     jobName,
			Status:      build.Status,
			Ref:         refValue,
			SHA:         shaValue,
			Author:      authorValue,
			CreatedAt:   build.CreatedAt,
			StartedAt:   build.StartedAt,
			FinishedAt:  build.FinishedAt,
			DurationMS:  durationMS,
			WebURL:      webURL,
			Error:       build.ErrorMessage,
			Pipeline:    build.PipelineName,
		},
		FailedStep: failedStep,
	}
	return payload
}

func writeBuildStatusHuman(w io.Writer, payload buildStatusPayload) error {
	build := payload.Build
	if _, err := fmt.Fprintf(w, "Build:   %s\n", build.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Project: %s\n", displayProjectLabel(build.ProjectName, build.ProjectID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Job:     %s\n", displayJobLabel(build.JobName, build.JobID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Status:  %s\n", build.Status); err != nil {
		return err
	}
	if build.Ref != nil {
		if _, err := fmt.Fprintf(w, "Ref:     %s\n", *build.Ref); err != nil {
			return err
		}
	}
	if build.SHA != nil {
		if _, err := fmt.Fprintf(w, "Commit:  %s\n", shortSHA(*build.SHA)); err != nil {
			return err
		}
	}
	if build.Author != nil {
		if _, err := fmt.Fprintf(w, "Author:  %s\n", *build.Author); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Created: %s\n", build.CreatedAt); err != nil {
		return err
	}
	if build.StartedAt != nil {
		if _, err := fmt.Fprintf(w, "Started: %s\n", *build.StartedAt); err != nil {
			return err
		}
	}
	if build.FinishedAt != nil {
		if _, err := fmt.Fprintf(w, "Finished:%s%s\n", strings.Repeat(" ", 1), *build.FinishedAt); err != nil {
			return err
		}
	}
	if build.DurationMS != nil {
		if _, err := fmt.Fprintf(w, "Duration:%s%s\n", strings.Repeat(" ", 1), formatDurationMS(*build.DurationMS)); err != nil {
			return err
		}
	}
	if payload.FailedStep != nil {
		failedSummary := fmt.Sprintf("step %d %s (%s)", payload.FailedStep.Index, payload.FailedStep.Name, payload.FailedStep.Status)
		if payload.FailedStep.ExitCode != nil {
			failedSummary = fmt.Sprintf("step %d %s exited %d", payload.FailedStep.Index, payload.FailedStep.Name, *payload.FailedStep.ExitCode)
		}
		if _, err := fmt.Fprintf(w, "Failed:  %s\n", failedSummary); err != nil {
			return err
		}
	}
	if build.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL:     %s\n", build.WebURL); err != nil {
			return err
		}
	}
	return nil
}

func writeBuildLogsHuman(w io.Writer, response api.BuildLogsResponse) error {
	if len(response.Logs) == 0 {
		_, err := fmt.Fprintln(w, "No logs found")
		return err
	}

	currentHeader := ""
	for _, entry := range response.Logs {
		header := logHeader(response.SelectedStep, entry)
		if header != currentHeader {
			if currentHeader != "" {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "== %s ==\n", header); err != nil {
				return err
			}
			currentHeader = header
		}

		line := entry.Line
		if line == "" {
			line = entry.Message
		}
		if entry.Stream != "" && entry.Stream != "stdout" {
			line = "[" + entry.Stream + "] " + line
		}
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if !strings.HasSuffix(line, "\n") {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
	}
	if response.Truncated {
		if _, err := fmt.Fprintln(w, "\n[truncated] Showing the most recent log entries."); err != nil {
			return err
		}
	}
	return nil
}

func logHeader(selected *api.BuildLogSelectedStepResponse, entry api.BuildLogResponse) string {
	if selected != nil {
		return fmt.Sprintf("step %d: %s (%s)", selected.StepIndex, selected.Name, selected.Status)
	}
	return fmt.Sprintf("step %d: %s", entry.StepIndex, entry.StepName)
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
