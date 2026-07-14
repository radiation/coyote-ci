package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

var isInteractiveInputFunc = isInteractiveInput

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
