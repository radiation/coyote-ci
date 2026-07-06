package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type jobListPayload struct {
	ProjectSelector string    `json:"project_selector"`
	Jobs            []jobView `json:"jobs"`
}

type jobPayload struct {
	Job jobView `json:"job"`
}

type jobRunPayload struct {
	Run jobRunView `json:"run"`
}

type jobView struct {
	ID             string              `json:"id"`
	ProjectID      string              `json:"project_id"`
	Name           string              `json:"name"`
	Priority       int                 `json:"priority"`
	RepositoryURL  string              `json:"repository_url"`
	DefaultRef     string              `json:"default_ref"`
	TriggerMode    string              `json:"trigger_mode"`
	Enabled        bool                `json:"enabled"`
	PipelinePath   *string             `json:"pipeline_path,omitempty"`
	PipelineSource string              `json:"pipeline_source,omitempty"`
	LatestBuild    *jobLatestBuildView `json:"latest_build,omitempty"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
	WebURL         string              `json:"web_url,omitempty"`
}

type jobLatestBuildView struct {
	ID           string  `json:"id"`
	BuildNumber  int64   `json:"build_number,omitempty"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	FinishedAt   *string `json:"finished_at,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
}

type jobRunView struct {
	JobID     string `json:"job_id"`
	JobName   string `json:"job_name"`
	ProjectID string `json:"project_id"`
	Ref       string `json:"ref"`
	BuildID   string `json:"build_id"`
	Status    string `json:"status"`
	WebURL    string `json:"web_url,omitempty"`
}

func (a *app) newJobCommand() *cobra.Command {
	command := &cobra.Command{Use: "job", Short: "Discover jobs"}
	command.AddCommand(a.newJobListCommand())
	command.AddCommand(a.newJobShowCommand())
	command.AddCommand(a.newJobRunCommand())
	return command
}

func (a *app) newJobListCommand() *cobra.Command {
	var projectSelector string

	command := &cobra.Command{
		Use:   "list",
		Short: "List jobs for one project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			response, listErr := client.ListJobs(cmd.Context(), projectSelector)
			if listErr != nil {
				return mapCommandError(listErr)
			}

			payload := makeJobListPayload(resolved.ServerURL, projectSelector, response.Jobs)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeJobListHuman(w, payload)
			}, payload)
		},
	}
	command.Flags().StringVar(&projectSelector, "project", "", "Project ID or slug")
	if err := command.MarkFlagRequired("project"); err != nil {
		panic(err)
	}
	return command
}

func (a *app) newJobShowCommand() *cobra.Command {
	var projectSelector string

	command := &cobra.Command{
		Use:   "show <job-id-or-name>",
		Short: "Show one job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := strings.TrimSpace(args[0])
			if selector == "" {
				return &ExitError{Code: 2, Err: errors.New("job selector is required")}
			}
			if strings.TrimSpace(projectSelector) == "" && !looksLikeDirectJobID(selector) {
				return &ExitError{Code: 2, Err: errors.New("job name requires --project; use a job id or pass --project")}
			}

			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			job, jobErr := client.GetJob(cmd.Context(), selector, apiclient.GetJobOptions{Project: projectSelector})
			if jobErr != nil {
				return mapCommandError(jobErr)
			}

			payload := makeJobPayload(resolved.ServerURL, job)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeJobHuman(w, payload)
			}, payload)
		},
	}
	command.Flags().StringVar(&projectSelector, "project", "", "Project ID or slug for name-based lookup")
	return command
}

func (a *app) newJobRunCommand() *cobra.Command {
	var projectSelector string
	var ref string
	var assumeYes bool

	command := &cobra.Command{
		Use:   "run <job-id-or-name>",
		Short: "Start a new build for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := strings.TrimSpace(args[0])
			if selector == "" {
				return &ExitError{Code: 2, Err: errors.New("job selector is required")}
			}
			trimmedRef := strings.TrimSpace(ref)
			if trimmedRef == "" {
				return &ExitError{Code: 2, Err: errors.New("ref is required")}
			}
			if strings.TrimSpace(projectSelector) == "" && !looksLikeDirectJobID(selector) {
				return &ExitError{Code: 2, Err: errors.New("job name requires --project; use a job id or pass --project")}
			}

			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			if validationErr := a.validateJobRunInvocation(resolved.OutputMode, assumeYes); validationErr != nil {
				return validationErr
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			getOptions := apiclient.GetJobOptions{}
			if !looksLikeDirectJobID(selector) {
				getOptions.Project = projectSelector
			}
			job, jobErr := client.GetJob(cmd.Context(), selector, getOptions)
			if jobErr != nil {
				return mapCommandError(jobErr)
			}

			confirmErr := a.confirmJobRun(job.Name, trimmedRef, assumeYes)
			if confirmErr != nil {
				return confirmErr
			}

			build, runErr := client.RunJob(cmd.Context(), job.ID, apiclient.RunJobOptions{Ref: trimmedRef})
			if runErr != nil {
				return mapCommandError(runErr)
			}

			payload := makeJobRunPayload(resolved.ServerURL, job, trimmedRef, build)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeJobRunHuman(w, payload)
			}, payload)
		},
	}
	command.Flags().StringVar(&projectSelector, "project", "", "Project ID or slug for name-based lookup")
	command.Flags().StringVar(&ref, "ref", "", "Branch, tag, or commit SHA to run")
	command.Flags().BoolVar(&assumeYes, "yes", false, "Skip confirmation prompt")
	return command
}

func makeJobListPayload(serverURL string, projectSelector string, jobs []api.JobResponse) jobListPayload {
	items := make([]jobView, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, makeJobView(serverURL, job))
	}
	return jobListPayload{ProjectSelector: strings.TrimSpace(projectSelector), Jobs: items}
}

func makeJobPayload(serverURL string, job api.JobResponse) jobPayload {
	return jobPayload{Job: makeJobView(serverURL, job)}
}

func makeJobRunPayload(serverURL string, job api.JobResponse, ref string, build api.BuildResponse) jobRunPayload {
	return jobRunPayload{
		Run: jobRunView{
			JobID:     job.ID,
			JobName:   strings.TrimSpace(job.Name),
			ProjectID: strings.TrimSpace(job.ProjectID),
			Ref:       strings.TrimSpace(ref),
			BuildID:   build.ID,
			Status:    build.Status,
			WebURL:    buildWebURL(serverURL, build.ID, nil),
		},
	}
}

func makeJobView(serverURL string, job api.JobResponse) jobView {
	pipelinePath := trimStringPtr(job.PipelinePath)
	pipelineSource := ""
	if pipelinePath != nil {
		pipelineSource = *pipelinePath
	} else if strings.TrimSpace(job.PipelineYAML) != "" {
		pipelineSource = "inline"
	}

	var latestBuild *jobLatestBuildView
	if job.LatestBuild != nil {
		latestBuild = &jobLatestBuildView{
			ID:           job.LatestBuild.ID,
			BuildNumber:  job.LatestBuild.BuildNumber,
			Status:       job.LatestBuild.Status,
			CreatedAt:    job.LatestBuild.CreatedAt,
			FinishedAt:   trimStringPtr(job.LatestBuild.FinishedAt),
			ErrorMessage: trimStringPtr(job.LatestBuild.ErrorMessage),
		}
	}

	return jobView{
		ID:             job.ID,
		ProjectID:      job.ProjectID,
		Name:           job.Name,
		Priority:       job.Priority,
		RepositoryURL:  job.RepositoryURL,
		DefaultRef:     job.DefaultRef,
		TriggerMode:    job.TriggerMode,
		Enabled:        job.Enabled,
		PipelinePath:   pipelinePath,
		PipelineSource: pipelineSource,
		LatestBuild:    latestBuild,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
		WebURL:         resourceWebURL(serverURL, "/jobs/"+url.PathEscape(strings.TrimSpace(job.ID))),
	}
}

func writeJobListHuman(w io.Writer, payload jobListPayload) error {
	if len(payload.Jobs) == 0 {
		_, err := fmt.Fprintln(w, "No jobs found.")
		return err
	}

	if payload.ProjectSelector != "" {
		if _, err := fmt.Fprintf(w, "Jobs for project %s\n", payload.ProjectSelector); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "Jobs"); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tNAME\tENABLED\tREF\tLATEST"); err != nil {
		return err
	}
	for _, job := range payload.Jobs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\n", job.ID, job.Name, job.Enabled, emptyOr(job.DefaultRef, "-"), latestBuildLabel(job.LatestBuild)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeJobHuman(w io.Writer, payload jobPayload) error {
	job := payload.Job
	if _, err := fmt.Fprintf(w, "Job:        %s\n", job.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "ID:         %s\n", job.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Project:    %s\n", job.ProjectID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Enabled:    %t\n", job.Enabled); err != nil {
		return err
	}
	if job.RepositoryURL != "" {
		if _, err := fmt.Fprintf(w, "Repo:       %s\n", job.RepositoryURL); err != nil {
			return err
		}
	}
	if job.DefaultRef != "" {
		if _, err := fmt.Fprintf(w, "Ref:        %s\n", job.DefaultRef); err != nil {
			return err
		}
	}
	if job.TriggerMode != "" {
		if _, err := fmt.Fprintf(w, "Trigger:    %s\n", job.TriggerMode); err != nil {
			return err
		}
	}
	if job.PipelineSource != "" {
		if _, err := fmt.Fprintf(w, "Pipeline:   %s\n", job.PipelineSource); err != nil {
			return err
		}
	}
	if job.LatestBuild != nil {
		if _, err := fmt.Fprintf(w, "Latest:     %s\n", latestBuildLongLabel(job.LatestBuild)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Created:    %s\n", job.CreatedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Updated:    %s\n", job.UpdatedAt); err != nil {
		return err
	}
	if job.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL:        %s\n", job.WebURL); err != nil {
			return err
		}
	}
	return nil
}

func writeJobRunHuman(w io.Writer, payload jobRunPayload) error {
	run := payload.Run
	jobLabel := run.JobName
	if jobLabel == "" {
		jobLabel = run.JobID
	}
	if _, err := fmt.Fprintf(w, "Started job %s\n", jobLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Build:  %s\n", run.BuildID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Status: %s\n", run.Status); err != nil {
		return err
	}
	if run.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL:    %s\n", run.WebURL); err != nil {
			return err
		}
	}
	if run.BuildID != "" {
		if _, err := fmt.Fprintf(w, "\nNext:\n  coyote build status %s\n", run.BuildID); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) validateJobRunInvocation(mode output.Mode, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	if mode == output.ModeJSON {
		return &ExitError{Code: 2, Err: errors.New("job run with --json requires --yes")}
	}
	if !isInteractiveInputFunc(a.stdin) {
		return &ExitError{Code: 2, Err: errors.New("job run requires --yes when stdin is not interactive")}
	}
	return nil
}

func (a *app) confirmJobRun(jobName string, ref string, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	if validationErr := a.validateJobRunInvocation(output.ModeHuman, assumeYes); validationErr != nil {
		return validationErr
	}
	trimmedJobName := strings.TrimSpace(jobName)
	if trimmedJobName == "" {
		trimmedJobName = "job"
	}
	if _, err := fmt.Fprintf(a.stderr, "Run job %s on ref %s? This will start a new build. [y/N] ", trimmedJobName, strings.TrimSpace(ref)); err != nil {
		return err
	}
	reader := bufio.NewReader(a.stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	trimmed := strings.ToLower(strings.TrimSpace(answer))
	if trimmed != "y" && trimmed != "yes" {
		return &ExitError{Code: 2, Err: errors.New("job run canceled")}
	}
	return nil
}

func latestBuildLabel(build *jobLatestBuildView) string {
	if build == nil {
		return "-"
	}
	if build.BuildNumber > 0 {
		return fmt.Sprintf("#%d %s", build.BuildNumber, build.Status)
	}
	return fmt.Sprintf("%s %s", build.ID, build.Status)
}

func latestBuildLongLabel(build *jobLatestBuildView) string {
	if build == nil {
		return "-"
	}
	if build.BuildNumber > 0 {
		return fmt.Sprintf("#%d %s", build.BuildNumber, build.Status)
	}
	return fmt.Sprintf("%s (%s)", build.ID, build.Status)
}

func looksLikeDirectJobID(selector string) bool {
	trimmedSelector := strings.TrimSpace(selector)
	if trimmedSelector == "" {
		return false
	}
	if _, err := uuid.Parse(trimmedSelector); err == nil {
		return true
	}
	return strings.HasPrefix(trimmedSelector, "job-")
}

func resourceWebURL(serverURL string, resourcePath string) string {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	basePath := strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = basePath + resourcePath
	return parsed.String()
}
