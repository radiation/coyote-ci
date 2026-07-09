package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/atomicfile"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

type buildStatusPayload struct {
	Build      buildStatusView      `json:"build"`
	FailedStep *buildFailedStepView `json:"failed_step,omitempty"`
}

type buildStatusView struct {
	ID           string                 `json:"id"`
	BuildNumber  int64                  `json:"build_number,omitempty"`
	ProjectID    string                 `json:"project_id"`
	ProjectName  *string                `json:"project_name,omitempty"`
	JobID        *string                `json:"job_id,omitempty"`
	JobName      *string                `json:"job_name,omitempty"`
	Status       string                 `json:"status"`
	Ref          *string                `json:"ref,omitempty"`
	SHA          *string                `json:"sha,omitempty"`
	Author       *string                `json:"author,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	StartedAt    *string                `json:"started_at,omitempty"`
	FinishedAt   *string                `json:"finished_at,omitempty"`
	DurationMS   *int64                 `json:"duration_ms,omitempty"`
	WebURL       string                 `json:"web_url"`
	Error        *string                `json:"error_message,omitempty"`
	Pipeline     *string                `json:"pipeline_name,omitempty"`
	CurrentSteps []buildCurrentStepView `json:"current_steps"`
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

var buildWatchPollInterval = 3 * time.Second

type buildWatchEvent struct {
	Type      string  `json:"type"`
	BuildID   string  `json:"build_id"`
	Timestamp string  `json:"timestamp"`
	Status    string  `json:"status,omitempty"`
	ExitCode  *int    `json:"exit_code,omitempty"`
	StepIndex *int    `json:"step_index,omitempty"`
	StepName  *string `json:"step_name,omitempty"`
	StepID    *string `json:"step_id,omitempty"`
	Stream    string  `json:"stream,omitempty"`
	Text      string  `json:"text,omitempty"`
}

type buildWatchEmitter struct {
	mode    output.Mode
	writer  io.Writer
	encoder *json.Encoder
	mu      sync.Mutex
}

func newBuildWatchEmitter(mode output.Mode, writer io.Writer) *buildWatchEmitter {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return &buildWatchEmitter{mode: mode, writer: writer, encoder: encoder}
}

var replaceFileAtomicFunc = atomicfile.ReplaceFileAtomic

var isInteractiveInputFunc = isInteractiveInput

func (a *app) newBuildCommand() *cobra.Command {
	command := &cobra.Command{Use: "build", Short: "Inspect and manage builds"}
	command.AddCommand(a.newBuildStatusCommand())
	command.AddCommand(a.newBuildWatchCommand())
	command.AddCommand(a.newBuildLogsCommand())
	command.AddCommand(a.newBuildArtifactTriggersCommand())
	command.AddCommand(a.newBuildArtifactsCommand())
	command.AddCommand(a.newBuildRetryCommand())
	return command
}

func (a *app) newBuildWatchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "watch <build-id>",
		Short: "Watch a build until it reaches a terminal state",
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

			emitter := newBuildWatchEmitter(resolved.OutputMode, a.stdout)
			terminalStatus, watchErr := watchBuild(cmd.Context(), client, emitter, args[0])
			if watchErr != nil {
				var exitErr *ExitError
				if errors.As(watchErr, &exitErr) {
					return watchErr
				}
				return mapCommandError(watchErr)
			}
			if terminalStatus == "success" {
				return nil
			}
			return &ExitError{Code: 1}
		},
	}
}

func (a *app) newBuildArtifactsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "artifacts <build-id>",
		Short: "List build artifacts",
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

			artifactsResponse, artifactsErr := client.ListBuildArtifacts(cmd.Context(), args[0])
			if artifactsErr != nil {
				return mapCommandError(artifactsErr)
			}

			payload := makeBuildArtifactsPayload(args[0], artifactsResponse.Artifacts)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildArtifactsHuman(w, payload)
			}, payload)
		},
	}
	command.AddCommand(a.newBuildArtifactsDownloadCommand())
	return command
}

func (a *app) newBuildArtifactTriggersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "artifact-triggers <build-id>",
		Short: "Inspect artifact-trigger deliveries for a build",
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

			response, responseErr := client.ListBuildArtifactTriggers(cmd.Context(), args[0])
			if responseErr != nil {
				return mapCommandError(responseErr)
			}

			payload := makeBuildArtifactTriggerDeliveriesPayload(args[0], response)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildArtifactTriggersHuman(w, payload)
			}, payload)
		},
	}
}

func (a *app) newBuildArtifactsDownloadCommand() *cobra.Command {
	var selector string
	var outputPath string
	var downloadAll bool
	var force bool

	command := &cobra.Command{
		Use:   "download <build-id>",
		Short: "Download build artifacts",
		Long: `Download one artifact by selector, or download all artifacts into a directory.

Use --artifact to select one artifact by ID, exact artifact path, name, or basename.
Use --all with --output <dir> to download every artifact while preserving safe artifact paths.
--artifact and --all are mutually exclusive.`,
		Example: `  coyote build artifacts download <build-id> --artifact report.xml
  coyote build artifacts download <build-id> --artifact artifacts/images/frontend-image.tar --output ./downloads/
  coyote build artifacts download <build-id> --all --output ./artifacts/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector = strings.TrimSpace(selector)
			bulkOutputDir := ""
			if downloadAll && selector != "" {
				return &ExitError{Code: 2, Err: errors.New("--artifact and --all are mutually exclusive")}
			}
			if !downloadAll && selector == "" {
				return &ExitError{Code: 2, Err: errors.New("one of --artifact or --all is required")}
			}
			if downloadAll {
				var dirErr error
				bulkOutputDir, dirErr = resolveArtifactBulkOutputDir(outputPath)
				if dirErr != nil {
					return &ExitError{Code: 2, Err: dirErr}
				}
			}

			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			artifactsResponse, artifactsErr := client.ListBuildArtifacts(cmd.Context(), args[0])
			if artifactsErr != nil {
				return mapCommandError(artifactsErr)
			}

			if downloadAll {
				if mkdirErr := os.MkdirAll(bulkOutputDir, 0o755); mkdirErr != nil {
					return &ExitError{Code: 1, Err: mkdirErr}
				}

				plannedDownloads, planErr := planBulkArtifactDownloads(artifactsResponse.Artifacts, bulkOutputDir, force)
				if planErr != nil {
					return &ExitError{Code: 2, Err: planErr}
				}

				payload := buildArtifactDownloadPayload{BuildID: args[0], Downloaded: make([]buildArtifactDownloadView, 0, len(plannedDownloads))}
				for _, planned := range plannedDownloads {
					written, downloadErr := downloadBuildArtifactToPath(cmd.Context(), client, args[0], planned.Artifact, planned.DestinationPath, force)
					if downloadErr != nil {
						var apiErr *apiclient.Error
						if errors.As(downloadErr, &apiErr) {
							return mapCommandError(downloadErr)
						}
						return &ExitError{Code: 1, Err: downloadErr}
					}
					payload.Downloaded = append(payload.Downloaded, buildArtifactDownloadViewFromArtifact(planned.Artifact, displayPath(planned.DestinationPath), written))
				}

				return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
					return writeBuildArtifactDownloadHuman(w, payload)
				}, payload)
			}

			artifact, selectErr := selectBuildArtifact(artifactsResponse.Artifacts, selector)
			if selectErr != nil {
				return &ExitError{Code: 2, Err: selectErr}
			}

			destinationPath, pathErr := resolveArtifactOutputPath(outputPath, artifact)
			if pathErr != nil {
				return &ExitError{Code: 2, Err: pathErr}
			}

			written, downloadErr := downloadBuildArtifactToPath(cmd.Context(), client, args[0], artifact, destinationPath, force)
			if downloadErr != nil {
				var apiErr *apiclient.Error
				if errors.As(downloadErr, &apiErr) {
					return mapCommandError(downloadErr)
				}
				return &ExitError{Code: 1, Err: downloadErr}
			}

			displayPath := displayPath(destinationPath)
			payload := buildArtifactDownloadPayload{
				BuildID:    args[0],
				Downloaded: []buildArtifactDownloadView{buildArtifactDownloadViewFromArtifact(artifact, displayPath, written)},
			}
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildArtifactDownloadHuman(w, payload)
			}, payload)
		},
	}
	command.Flags().StringVar(&selector, "artifact", "", "Artifact selector: ID first, exact path second, then name or basename; ambiguous matches require ID or full path")
	command.Flags().BoolVar(&downloadAll, "all", false, "Download all artifacts for the build")
	command.Flags().StringVar(&outputPath, "output", "", "Output file or directory path; required as a directory with --all")
	command.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
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
			options, err := parseBuildLogsOptions(stepRaw, failed, tail, cmd.Flags().Changed("tail"))
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

func (a *app) newBuildRetryCommand() *cobra.Command {
	var assumeYes bool

	command := &cobra.Command{
		Use:     "retry <build-id>",
		Aliases: []string{"rerun"},
		Short:   "Retry a whole build by creating a new attempt",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := a.resolveTarget()
			if err != nil {
				return err
			}
			confirmErr := a.confirmBuildRetry(args[0], resolved.OutputMode, assumeYes)
			if confirmErr != nil {
				return confirmErr
			}

			client, err := a.newClient(resolved)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}

			build, rerunErr := client.RerunBuild(cmd.Context(), args[0])
			if rerunErr != nil {
				return mapCommandError(rerunErr)
			}

			payload := makeBuildRetryPayload(resolved.ServerURL, args[0], build)
			return output.Write(resolved.OutputMode, a.stdout, func(w io.Writer) error {
				return writeBuildRetryHuman(w, payload)
			}, payload)
		},
	}
	command.Flags().BoolVar(&assumeYes, "yes", false, "Skip confirmation prompt")
	return command
}

func parseBuildLogsOptions(stepRaw string, failed bool, tail int, tailSet bool) (apiclient.BuildLogsOptions, error) {
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
	if tail < 0 || (tailSet && tail == 0) {
		return apiclient.BuildLogsOptions{}, fmt.Errorf("tail must be a positive integer")
	}
	options.Failed = failed
	options.Tail = tail
	return options, nil
}

func watchBuild(ctx context.Context, client *apiclient.Client, emitter *buildWatchEmitter, buildID string) (string, error) {
	logsCtx, cancelLogs := context.WithCancel(ctx)

	var streamWG sync.WaitGroup
	defer streamWG.Wait()
	defer cancelLogs()
	streamErrors := make(chan error, 1)

	activeStreams := make(map[int]struct{})
	var activeStreamsMu sync.Mutex
	var logsUnavailableOnce sync.Once

	logsEnabled := func() bool {
		return logsCtx.Err() == nil
	}

	reportStreamError := func(err error) {
		if err == nil {
			return
		}
		select {
		case streamErrors <- err:
		default:
		}
	}

	markLogsUnavailable := func() {
		logsUnavailableOnce.Do(func() {
			cancelLogs()
			reportStreamError(emitter.emit(buildWatchEvent{
				Type:      "logs_unavailable",
				BuildID:   buildID,
				Timestamp: watchTimestamp(nil),
			}))
		})
	}

	startStepStream := func(step api.BuildStepResponse) {
		if !logsEnabled() || step.Status != "running" {
			return
		}
		activeStreamsMu.Lock()
		if _, ok := activeStreams[step.StepIndex]; ok {
			activeStreamsMu.Unlock()
			return
		}
		activeStreams[step.StepIndex] = struct{}{}
		activeStreamsMu.Unlock()

		streamWG.Add(1)
		go func(step api.BuildStepResponse) {
			defer streamWG.Done()

			var after int64
			for {
				streamErr := client.StreamBuildStepLogs(logsCtx, buildID, step.StepIndex, after, func(event apiclient.StepLogStreamEvent) error {
					if event.Chunk == nil {
						return nil
					}
					after = maxInt64(after, event.Chunk.SequenceNo)
					return emitter.emit(buildWatchLogChunkEvent(buildID, *event.Chunk))
				})
				if streamErr == nil {
					if logsCtx.Err() != nil {
						return
					}
				} else {
					if logsCtx.Err() != nil || errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded) {
						return
					}
					if errors.Is(streamErr, apiclient.ErrStepLogStreamTimeout) {
						continue
					}

					var apiErr *apiclient.Error
					if errors.As(streamErr, &apiErr) && apiErr.Kind == apiclient.ErrorKindAuthorization && apiErr.Code == "missing_token_scope" {
						markLogsUnavailable()
						return
					}
					reportStreamError(streamErr)
					return
				}

				select {
				case <-logsCtx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}(step)
	}

	initialized := false
	previousStatus := ""
	previousSteps := map[int]api.BuildStepResponse{}

	for {
		select {
		case streamErr := <-streamErrors:
			return "", streamErr
		default:
		}

		build, err := client.GetBuild(ctx, buildID)
		if err != nil {
			return "", err
		}
		steps, err := client.GetBuildSteps(ctx, buildID)
		if err != nil {
			return "", err
		}

		sortedSteps := sortBuildSteps(steps)
		stepLookup := make(map[int]api.BuildStepResponse, len(sortedSteps))
		for _, step := range sortedSteps {
			stepLookup[step.StepIndex] = step
		}

		if !initialized || build.Status != previousStatus {
			if emitErr := emitter.emit(buildWatchEvent{
				Type:      "build_status",
				BuildID:   buildID,
				Timestamp: watchTimestamp(nil),
				Status:    strings.TrimSpace(build.Status),
			}); emitErr != nil {
				return "", emitErr
			}
		}

		for _, step := range sortedSteps {
			prev, hadPrev := previousSteps[step.StepIndex]
			if step.Status == "running" {
				if !initialized || !hadPrev || prev.Status != step.Status {
					if emitErr := emitter.emit(buildWatchStepEvent("step_started", buildID, step, nil)); emitErr != nil {
						return "", emitErr
					}
				}
				startStepStream(step)
				continue
			}

			if initialized && hadPrev && prev.Status != step.Status && isTerminalStepStatus(step.Status) {
				exitCode := step.ExitCode
				if emitErr := emitter.emit(buildWatchStepEvent("step_finished", buildID, step, exitCode)); emitErr != nil {
					return "", emitErr
				}
			}
		}

		initialized = true
		previousStatus = build.Status
		previousSteps = stepLookup

		if isTerminalBuildStatus(build.Status) {
			cancelLogs()
			status := strings.TrimSpace(build.Status)
			exitCode := buildWatchExitCode(status)
			if emitErr := emitter.emit(buildWatchEvent{
				Type:      "terminal",
				BuildID:   buildID,
				Timestamp: watchTimestamp(build.FinishedAt),
				Status:    status,
				ExitCode:  &exitCode,
			}); emitErr != nil {
				return "", emitErr
			}
			return status, nil
		}

		select {
		case streamErr := <-streamErrors:
			return "", streamErr
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(buildWatchPollInterval):
		}
	}
}

func (e *buildWatchEmitter) emit(event buildWatchEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.mode == output.ModeJSON {
		return e.encoder.Encode(event)
	}
	return e.writeHuman(event)
}

func (e *buildWatchEmitter) writeHuman(event buildWatchEvent) error {
	switch event.Type {
	case "build_status":
		_, err := fmt.Fprintf(e.writer, "Build %s: %s\n", event.BuildID, event.Status)
		return err
	case "step_started":
		_, err := fmt.Fprintf(e.writer, "==> step %d: %s started\n", valueOrZero(event.StepIndex), valueOrUnknownPtr(event.StepName))
		return err
	case "step_finished":
		line := fmt.Sprintf("<== step %d: %s %s", valueOrZero(event.StepIndex), valueOrUnknownPtr(event.StepName), event.Status)
		if event.ExitCode != nil {
			line += fmt.Sprintf(" (exit code %d)", *event.ExitCode)
		}
		_, err := fmt.Fprintln(e.writer, line)
		return err
	case "log_chunk":
		prefix := fmt.Sprintf("[step %d %s] ", valueOrZero(event.StepIndex), valueOrUnknownPtr(event.StepName))
		if event.Stream != "" && event.Stream != "stdout" {
			prefix = fmt.Sprintf("[step %d %s][%s] ", valueOrZero(event.StepIndex), valueOrUnknownPtr(event.StepName), event.Stream)
		}
		text := event.Text
		if _, err := io.WriteString(e.writer, prefix+text); err != nil {
			return err
		}
		if strings.HasSuffix(text, "\n") {
			return nil
		}
		_, err := fmt.Fprintln(e.writer)
		return err
	case "logs_unavailable":
		_, err := fmt.Fprintln(e.writer, "Live logs unavailable for this token; continuing with status-only watch.")
		return err
	case "terminal":
		_, err := fmt.Fprintf(e.writer, "Build %s completed with status %s (exit %d)\n", event.BuildID, event.Status, valueOrZero(event.ExitCode))
		return err
	default:
		return nil
	}
}

func makeBuildArtifactsPayload(buildID string, artifacts []api.BuildArtifactResponse) buildArtifactsPayload {
	items := make([]buildArtifactListView, 0, len(artifacts))
	for _, artifact := range sortBuildArtifacts(artifacts) {
		stepID := trimStringPtr(artifact.StepID)
		item := buildArtifactListView{
			ID:          artifact.ID,
			Name:        strings.TrimSpace(artifact.Name),
			Path:        artifact.Path,
			StepID:      stepID,
			SizeBytes:   artifact.SizeBytes,
			ContentType: trimStringPtr(artifact.ContentType),
			CreatedAt:   artifact.CreatedAt,
		}
		items = append(items, item)
	}

	resolvedBuildID := strings.TrimSpace(buildID)
	if len(artifacts) > 0 && strings.TrimSpace(artifacts[0].BuildID) != "" {
		resolvedBuildID = artifacts[0].BuildID
	}
	return buildArtifactsPayload{BuildID: resolvedBuildID, Artifacts: items}
}

func makeBuildArtifactTriggerDeliveriesPayload(buildID string, response api.BuildArtifactTriggerDeliveriesResponse) buildArtifactTriggerDeliveriesPayload {
	deliveries := make([]buildArtifactTriggerDeliveryView, 0, len(response.Deliveries))
	for _, delivery := range response.Deliveries {
		deliveries = append(deliveries, buildArtifactTriggerDeliveryView{
			DeliveryID:        delivery.DeliveryID,
			Status:            delivery.Status,
			CreatedAt:         delivery.CreatedAt,
			UpdatedAt:         delivery.UpdatedAt,
			ProducerBuildID:   delivery.ProducerBuildID,
			ProducerProjectID: delivery.ProducerProjectID,
			ProducerJobID:     delivery.ProducerJobID,
			ArtifactID:        delivery.ArtifactID,
			ArtifactPath:      delivery.ArtifactPath,
			ArtifactName:      trimStringPtr(delivery.ArtifactName),
			ArtifactSizeBytes: delivery.ArtifactSizeBytes,
			ConsumerJobID:     delivery.ConsumerJobID,
			ConsumerJobName:   trimStringPtr(delivery.ConsumerJobName),
			DownstreamBuildID: trimStringPtr(delivery.DownstreamBuildID),
			ErrorMessage:      trimStringPtr(delivery.ErrorMessage),
		})
	}
	resolvedBuildID := strings.TrimSpace(buildID)
	if trimmedResponseBuildID := strings.TrimSpace(response.BuildID); trimmedResponseBuildID != "" {
		resolvedBuildID = trimmedResponseBuildID
	}
	return buildArtifactTriggerDeliveriesPayload{
		BuildID:                  resolvedBuildID,
		BuildTriggerKind:         strings.TrimSpace(response.BuildTriggerKind),
		RecursiveDispatchBlocked: response.RecursiveDispatchBlocked,
		Summary: buildArtifactTriggerSummaryView{
			DeliveryCount: response.Summary.DeliveryCount,
			QueuedCount:   response.Summary.QueuedCount,
			FailedCount:   response.Summary.FailedCount,
		},
		Deliveries: deliveries,
	}
}

func makeBuildStatusPayload(serverURL string, build api.BuildResponse, steps []api.BuildStepResponse) buildStatusPayload {
	jobName := firstNonEmptyPtr(build.JobName, firstJobName(steps))
	refValue := firstNonEmptyPtr(build.SourceRef, build.TriggerRef)
	shaValue := firstNonEmptyPtr(build.SourceCommitSHA, build.SourceSHA, build.TriggerCommitSHA)
	authorValue := firstNonEmptyPtr(build.SourceAuthorName, build.TriggeredBy, build.Actor)
	failedStep := firstFailedStep(steps)
	durationMS := buildDurationMS(build.StartedAt, build.FinishedAt)
	webURL := buildWebURL(serverURL, build.ID, failedStep)
	currentSteps := makeBuildCurrentStepViews(build.CurrentSteps)

	payload := buildStatusPayload{
		Build: buildStatusView{
			ID:           build.ID,
			BuildNumber:  build.BuildNumber,
			ProjectID:    build.ProjectID,
			ProjectName:  build.ProjectName,
			JobID:        build.JobID,
			JobName:      jobName,
			Status:       build.Status,
			Ref:          refValue,
			SHA:          shaValue,
			Author:       authorValue,
			CreatedAt:    build.CreatedAt,
			StartedAt:    build.StartedAt,
			FinishedAt:   build.FinishedAt,
			DurationMS:   durationMS,
			WebURL:       webURL,
			Error:        build.ErrorMessage,
			Pipeline:     build.PipelineName,
			CurrentSteps: currentSteps,
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
	if len(build.CurrentSteps) > 0 {
		if _, err := fmt.Fprintln(w, "\nRunning:"); err != nil {
			return err
		}
		for _, step := range build.CurrentSteps {
			if _, err := fmt.Fprintf(w, "  [%d] %s\n", step.Index, step.Name); err != nil {
				return err
			}
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

func writeBuildArtifactsHuman(w io.Writer, payload buildArtifactsPayload) error {
	if _, err := fmt.Fprintf(w, "Artifacts for build %s\n", payload.BuildID); err != nil {
		return err
	}
	if len(payload.Artifacts) == 0 {
		_, err := fmt.Fprintln(w, "\nNo artifacts found")
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSTEP ID\tPATH\tSIZE\tTYPE"); err != nil {
		return err
	}
	for _, artifact := range payload.Artifacts {
		step := "-"
		if artifact.StepID != nil {
			step = *artifact.StepID
		}
		contentType := "-"
		if artifact.ContentType != nil && strings.TrimSpace(*artifact.ContentType) != "" {
			contentType = *artifact.ContentType
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", artifact.ID, step, artifact.Path, formatArtifactSize(artifact.SizeBytes), contentType); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nDownload:\n  coyote build artifacts download %s --artifact %s\n", payload.BuildID, payload.Artifacts[0].ID); err != nil {
		return err
	}
	return nil
}

func writeBuildArtifactTriggersHuman(w io.Writer, payload buildArtifactTriggerDeliveriesPayload) error {
	if _, err := fmt.Fprintf(w, "Artifact trigger deliveries for build %s\n", payload.BuildID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Trigger: %s\n", displayTriggerKind(payload.BuildTriggerKind)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Summary: %d deliveries, %d queued, %d failed\n", payload.Summary.DeliveryCount, payload.Summary.QueuedCount, payload.Summary.FailedCount); err != nil {
		return err
	}
	if len(payload.Deliveries) == 0 {
		if payload.RecursiveDispatchBlocked {
			_, err := fmt.Fprintln(w, "\nRecursive artifact-trigger dispatch is blocked for artifact-triggered builds.")
			return err
		}
		_, err := fmt.Fprintln(w, "\nNo artifact-trigger deliveries were recorded for this build.")
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STATUS\tARTIFACT\tCONSUMER JOB\tDOWNSTREAM BUILD\tERROR"); err != nil {
		return err
	}
	for _, delivery := range payload.Deliveries {
		errorValue := "-"
		if delivery.ErrorMessage != nil {
			errorValue = *delivery.ErrorMessage
		}
		downstreamBuild := "-"
		if delivery.DownstreamBuildID != nil {
			downstreamBuild = *delivery.DownstreamBuildID
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", delivery.Status, displayArtifactTriggerArtifact(delivery), displayArtifactTriggerConsumerJob(delivery), downstreamBuild, errorValue); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func makeBuildRetryPayload(serverURL string, sourceBuildID string, build api.BuildResponse) buildRetryPayload {
	return buildRetryPayload{
		Retried: buildRetryView{
			SourceBuildID: strings.TrimSpace(sourceBuildID),
			BuildID:       build.ID,
			Status:        build.Status,
			WebURL:        buildWebURL(serverURL, build.ID, nil),
		},
	}
}

func writeBuildRetryHuman(w io.Writer, payload buildRetryPayload) error {
	retried := payload.Retried
	if retried.SourceBuildID != "" && retried.SourceBuildID != retried.BuildID {
		if _, err := fmt.Fprintf(w, "Retried build %s -> %s\n", retried.SourceBuildID, retried.BuildID); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "Retried build %s\n", retried.BuildID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Status: %s\n", retried.Status); err != nil {
		return err
	}
	if retried.WebURL != "" {
		if _, err := fmt.Fprintf(w, "URL: %s\n", retried.WebURL); err != nil {
			return err
		}
	}
	return nil
}

func writeBuildArtifactDownloadHuman(w io.Writer, payload buildArtifactDownloadPayload) error {
	if len(payload.Downloaded) == 0 {
		if _, err := fmt.Fprintf(w, "No artifacts found for build %s\n", strings.TrimSpace(payload.BuildID)); err != nil {
			return err
		}
		return nil
	}
	for _, item := range payload.Downloaded {
		if _, err := fmt.Fprintf(w, "Downloaded %s -> %s\n", item.Name, item.Path); err != nil {
			return err
		}
	}
	return nil
}

func displayArtifactTriggerArtifact(delivery buildArtifactTriggerDeliveryView) string {
	if delivery.ArtifactName != nil {
		return *delivery.ArtifactName
	}
	if trimmed := strings.TrimSpace(delivery.ArtifactPath); trimmed != "" {
		return trimmed
	}
	return delivery.ArtifactID
}

func displayArtifactTriggerConsumerJob(delivery buildArtifactTriggerDeliveryView) string {
	if delivery.ConsumerJobName != nil {
		return *delivery.ConsumerJobName
	}
	return delivery.ConsumerJobID
}

func displayTriggerKind(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func (a *app) confirmBuildRetry(buildID string, mode output.Mode, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	if mode == output.ModeJSON {
		return &ExitError{Code: 2, Err: errors.New("build retry with --json requires --yes")}
	}
	if !isInteractiveInputFunc(a.stdin) {
		return &ExitError{Code: 2, Err: errors.New("build retry requires --yes when stdin is not interactive")}
	}
	if _, err := fmt.Fprintf(a.stderr, "Retry build %s? This may start a new build. [y/N] ", strings.TrimSpace(buildID)); err != nil {
		return err
	}
	reader := bufio.NewReader(a.stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	trimmed := strings.ToLower(strings.TrimSpace(answer))
	if trimmed != "y" && trimmed != "yes" {
		return &ExitError{Code: 2, Err: errors.New("build retry canceled")}
	}
	return nil
}

func isInteractiveInput(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func sortBuildArtifacts(artifacts []api.BuildArtifactResponse) []api.BuildArtifactResponse {
	sorted := append([]api.BuildArtifactResponse(nil), artifacts...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt != sorted[j].CreatedAt {
			return sorted[i].CreatedAt > sorted[j].CreatedAt
		}
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
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

func selectBuildArtifact(artifacts []api.BuildArtifactResponse, selector string) (api.BuildArtifactResponse, error) {
	trimmed := strings.TrimSpace(selector)
	for _, artifact := range artifacts {
		if artifact.ID == trimmed {
			return artifact, nil
		}
	}

	pathMatches := make([]api.BuildArtifactResponse, 0)
	nameMatches := make([]api.BuildArtifactResponse, 0)
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) == trimmed {
			pathMatches = append(pathMatches, artifact)
		}
		if artifactNameMatches(artifact, trimmed) {
			nameMatches = append(nameMatches, artifact)
		}
	}

	if len(pathMatches) == 1 {
		return pathMatches[0], nil
	}
	if len(pathMatches) > 1 {
		return api.BuildArtifactResponse{}, fmt.Errorf("artifact selector %q matched multiple artifact paths; use an artifact ID", trimmed)
	}
	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		return api.BuildArtifactResponse{}, fmt.Errorf("artifact selector %q matched multiple artifact names; use an artifact ID or full path", trimmed)
	}
	return api.BuildArtifactResponse{}, fmt.Errorf("artifact %q not found for build", trimmed)
}

func artifactNameMatches(artifact api.BuildArtifactResponse, selector string) bool {
	if strings.TrimSpace(artifact.Name) == selector {
		return true
	}
	return path.Base(strings.TrimSpace(artifact.Path)) == selector
}

func resolveArtifactOutputPath(outputPath string, artifact api.BuildArtifactResponse) (string, error) {
	trimmedOutput := strings.TrimSpace(outputPath)
	if trimmedOutput == "" {
		defaultName, err := artifactDownloadName(artifact)
		if err != nil {
			return "", err
		}
		return defaultName, nil
	}

	if info, err := os.Stat(trimmedOutput); err == nil {
		if info.IsDir() {
			defaultName, nameErr := artifactDownloadName(artifact)
			if nameErr != nil {
				return "", nameErr
			}
			return filepath.Join(trimmedOutput, defaultName), nil
		}
		return trimmedOutput, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if strings.HasSuffix(trimmedOutput, string(os.PathSeparator)) || strings.HasSuffix(trimmedOutput, "/") {
		defaultName, nameErr := artifactDownloadName(artifact)
		if nameErr != nil {
			return "", nameErr
		}
		return filepath.Join(trimmedOutput, defaultName), nil
	}
	return trimmedOutput, nil
}

func resolveArtifactBulkOutputDir(outputPath string) (string, error) {
	trimmedOutput := strings.TrimSpace(outputPath)
	if trimmedOutput == "" {
		return "", errors.New("--all requires --output <dir>")
	}

	info, err := os.Stat(trimmedOutput)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("bulk download output must be a directory: %s", trimmedOutput)
		}
		return trimmedOutput, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return trimmedOutput, nil
}

func artifactDownloadName(artifact api.BuildArtifactResponse) (string, error) {
	relativePath, err := artifactDownloadRelativePath(artifact)
	if err != nil {
		return "", err
	}
	base := path.Base(relativePath)
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("artifact path %q does not resolve to a safe filename", artifact.Path)
	}
	return base, nil
}

func artifactDownloadRelativePath(artifact api.BuildArtifactResponse) (string, error) {
	for _, candidate := range []string{strings.TrimSpace(artifact.Path), strings.TrimSpace(artifact.Name), strings.TrimSpace(artifact.ID)} {
		if candidate == "" {
			continue
		}
		relativePath, err := validateArtifactRelativePath(candidate)
		if err != nil {
			return "", err
		}
		return relativePath, nil
	}
	return "", errors.New("artifact does not have a safe local filename")
}

func validateArtifactRelativePath(candidate string) (string, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(candidate, "\\", "/"))
	if normalized == "" {
		return "", errors.New("artifact does not have a safe local filename")
	}
	if path.IsAbs(normalized) || strings.HasPrefix(normalized, "/") || hasWindowsDrivePrefix(normalized) {
		return "", fmt.Errorf("artifact path %q is not safe for local output", candidate)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", fmt.Errorf("artifact path %q is not safe for local output", candidate)
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("artifact path %q is not safe for local output", candidate)
	}
	return cleaned, nil
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

type plannedArtifactDownload struct {
	Artifact        api.BuildArtifactResponse
	DestinationPath string
}

func planBulkArtifactDownloads(artifacts []api.BuildArtifactResponse, outputDir string, force bool) ([]plannedArtifactDownload, error) {
	trimmedOutputDir := strings.TrimSpace(outputDir)
	if trimmedOutputDir == "" {
		return nil, errors.New("bulk download output directory is required")
	}

	planned := make([]plannedArtifactDownload, 0, len(artifacts))
	seenPaths := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		relativePath, err := artifactDownloadRelativePath(artifact)
		if err != nil {
			return nil, err
		}
		destinationPath := filepath.Clean(filepath.Join(trimmedOutputDir, filepath.FromSlash(relativePath)))
		if existingArtifactID, exists := seenPaths[destinationPath]; exists {
			return nil, fmt.Errorf("artifacts %q and %q map to the same output path: %s", existingArtifactID, strings.TrimSpace(artifact.ID), destinationPath)
		}
		seenPaths[destinationPath] = strings.TrimSpace(artifact.ID)

		if info, err := os.Stat(destinationPath); err == nil {
			if info.IsDir() {
				return nil, fmt.Errorf("output path already exists as a directory: %s", destinationPath)
			}
			if !force {
				return nil, fmt.Errorf("output file already exists: %s (use --force to overwrite)", destinationPath)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		planned = append(planned, plannedArtifactDownload{Artifact: artifact, DestinationPath: destinationPath})
	}

	return planned, nil
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.count += int64(n)
	return n, err
}

func downloadBuildArtifactToPath(ctx context.Context, client *apiclient.Client, buildID string, artifact api.BuildArtifactResponse, destinationPath string, force bool) (int64, error) {
	trimmedDestination := strings.TrimSpace(destinationPath)
	if trimmedDestination == "" {
		return 0, errors.New("output path is required")
	}

	parentDir := filepath.Dir(trimmedDestination)
	if parentDir == "" {
		parentDir = "."
	}
	if parentDir != "." {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return 0, err
		}
	}
	destinationPerm := os.FileMode(0o600)
	if info, err := os.Stat(trimmedDestination); err == nil {
		if info.IsDir() {
			return 0, fmt.Errorf("output path already exists as a directory: %s", trimmedDestination)
		}
		destinationPerm = info.Mode().Perm()
		if !force {
			return 0, fmt.Errorf("output file already exists: %s (use --force to overwrite)", trimmedDestination)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if !force {
		if _, err := os.Stat(trimmedDestination); err == nil {
			return 0, fmt.Errorf("output file already exists: %s (use --force to overwrite)", trimmedDestination)
		}
	}

	tempFile, err := os.CreateTemp(parentDir, ".coyote-artifact-*")
	if err != nil {
		return 0, err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if err := tempFile.Chmod(destinationPerm); err != nil {
		return 0, err
	}

	counting := &countingWriter{writer: tempFile}
	if err := client.DownloadBuildArtifact(ctx, buildID, artifact.ID, counting); err != nil {
		_ = tempFile.Close()
		return 0, err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return 0, err
	}
	if err := tempFile.Close(); err != nil {
		return 0, err
	}
	if err := replaceFileAtomicFunc(tempPath, trimmedDestination); err != nil {
		return 0, err
	}
	return counting.count, nil
}

func buildArtifactDownloadViewFromArtifact(artifact api.BuildArtifactResponse, destinationPath string, written int64) buildArtifactDownloadView {
	name, err := artifactDownloadName(artifact)
	if err != nil {
		name = strings.TrimSpace(artifact.ID)
	}
	return buildArtifactDownloadView{
		ArtifactID:      artifact.ID,
		Name:            name,
		ArtifactPath:    strings.TrimSpace(artifact.Path),
		StepID:          trimStringPtr(artifact.StepID),
		ContentType:     trimStringPtr(artifact.ContentType),
		SizeBytes:       artifact.SizeBytes,
		Path:            destinationPath,
		LocalPath:       destinationPath,
		DownloadedBytes: written,
	}
}

func displayPath(pathValue string) string {
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" || filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, ".") {
		return trimmed
	}
	return "." + string(os.PathSeparator) + trimmed
}

func formatArtifactSize(sizeBytes int64) string {
	if sizeBytes < 1024 {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	size := float64(sizeBytes)
	unitIndex := -1
	for size >= 1024 && unitIndex < len(units)-1 {
		size /= 1024
		unitIndex++
	}
	if size >= 10 || size == float64(int64(size)) {
		return fmt.Sprintf("%.0f %s", size, units[unitIndex])
	}
	return fmt.Sprintf("%.1f %s", size, units[unitIndex])
}

func logHeader(selected *api.BuildLogSelectedStepResponse, entry api.BuildLogResponse) string {
	if selected != nil {
		return fmt.Sprintf("step %d: %s (%s)", selected.StepIndex, selected.Name, selected.Status)
	}
	return fmt.Sprintf("step %d: %s", entry.StepIndex, entry.StepName)
}

func buildWatchLogChunkEvent(buildID string, chunk api.StepLogChunkResponse) buildWatchEvent {
	stepIndex := chunk.StepIndex
	stepName := trimStringPtr(&chunk.StepName)
	stepID := trimStringPtr(&chunk.StepID)
	return buildWatchEvent{
		Type:      "log_chunk",
		BuildID:   buildID,
		Timestamp: watchTimestamp(&chunk.CreatedAt),
		StepIndex: &stepIndex,
		StepName:  stepName,
		StepID:    stepID,
		Stream:    strings.TrimSpace(chunk.Stream),
		Text:      chunk.ChunkText,
	}
}

func buildWatchStepEvent(eventType string, buildID string, step api.BuildStepResponse, exitCode *int) buildWatchEvent {
	stepIndex := step.StepIndex
	stepName := trimStringPtr(&step.Name)
	stepID := trimStringPtr(&step.ID)
	timestampSource := step.StartedAt
	if eventType == "step_finished" {
		timestampSource = step.FinishedAt
	}
	return buildWatchEvent{
		Type:      eventType,
		BuildID:   buildID,
		Timestamp: watchTimestamp(timestampSource),
		Status:    strings.TrimSpace(step.Status),
		ExitCode:  exitCode,
		StepIndex: &stepIndex,
		StepName:  stepName,
		StepID:    stepID,
	}
}

func watchTimestamp(value *string) string {
	if value != nil {
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			return trimmed
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func buildWatchExitCode(status string) int {
	if strings.TrimSpace(status) == "success" {
		return 0
	}
	return 1
}

func isTerminalBuildStatus(status string) bool {
	trimmed := strings.TrimSpace(status)
	return trimmed == "success" || trimmed == "failed" || trimmed == "canceled"
}

func isTerminalStepStatus(status string) bool {
	trimmed := strings.TrimSpace(status)
	return trimmed == "success" || trimmed == "failed" || trimmed == "canceled"
}

func sortBuildSteps(steps []api.BuildStepResponse) []api.BuildStepResponse {
	sorted := append([]api.BuildStepResponse(nil), steps...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StepIndex == sorted[j].StepIndex {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StepIndex < sorted[j].StepIndex
	})
	return sorted
}

func maxInt64(left int64, right int64) int64 {
	if right > left {
		return right
	}
	return left
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func valueOrUnknownPtr(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "unknown"
	}
	return strings.TrimSpace(*value)
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
