package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

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
