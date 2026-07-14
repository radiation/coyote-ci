package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

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
