package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/cli/output"
	releases "github.com/radiation/coyote-ci/backend/internal/release"
)

func (a *app) newReleaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "release", Short: "Resolve and publish Coyote software releases"}
	command.AddCommand(a.newReleaseResolveCommand())
	command.AddCommand(a.newReleasePublishCommand())
	command.AddCommand(a.newApplicationReleaseShowCommand())
	command.AddCommand(a.newApplicationReleasePromoteCommand())
	return command
}

func (a *app) newApplicationReleaseShowCommand() *cobra.Command {
	return &cobra.Command{Use: "show <release-id>", Short: "Show a generic application release", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		target, err := a.resolveTarget()
		if err != nil {
			return err
		}
		client, err := a.newClient(target)
		if err != nil {
			return &ExitError{Code: 3, Err: err}
		}
		release, err := client.GetApplicationRelease(cmd.Context(), args[0])
		if err != nil {
			return mapCommandError(err)
		}
		return output.Write(target.OutputMode, a.stdout, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "Release: %s\nVersion: %s\nStatus:  %s\n", release.ID, release.ApplicationVersion, release.Status)
			return err
		}, release)
	}}
}

func (a *app) newApplicationReleasePromoteCommand() *cobra.Command {
	var channel string
	command := &cobra.Command{Use: "promote <release-id>", Short: "Promote a complete generic application release", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if channel != "stable" {
			return &ExitError{Code: 2, Err: errors.New("only --channel stable is supported")}
		}
		target, err := a.resolveTarget()
		if err != nil {
			return err
		}
		client, err := a.newClient(target)
		if err != nil {
			return &ExitError{Code: 3, Err: err}
		}
		release, err := client.PromoteApplicationReleaseStable(cmd.Context(), args[0])
		if err != nil {
			return mapCommandError(err)
		}
		return output.Write(target.OutputMode, a.stdout, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "Promoted release %s to stable\n", release.ID)
			return err
		}, release)
	}}
	command.Flags().StringVar(&channel, "channel", "stable", "Channel to promote")
	return command
}

func (a *app) newReleaseResolveCommand() *cobra.Command {
	var releaseVersion, channel, sourceLocation, outputPath string
	command := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a Coyote release to immutable Helm values",
		RunE: func(cmd *cobra.Command, args []string) error {
			if (strings.TrimSpace(releaseVersion) == "") == (strings.TrimSpace(channel) == "") {
				return &ExitError{Code: 2, Err: errors.New("exactly one of --release or --channel is required")}
			}
			if strings.TrimSpace(sourceLocation) == "" || strings.TrimSpace(outputPath) == "" {
				return &ExitError{Code: 2, Err: errors.New("--release-source and --output are required")}
			}
			source, sourceErr := releases.NewFileSource(sourceLocation)
			if sourceErr != nil {
				return &ExitError{Code: 2, Err: sourceErr}
			}
			resolved, resolveErr := releases.Resolve(cmd.Context(), source, releaseVersion, channel)
			if resolveErr != nil {
				return &ExitError{Code: 1, Err: resolveErr}
			}
			writeErr := writeReleaseOverlay(outputPath, resolved)
			if writeErr != nil {
				return &ExitError{Code: 1, Err: writeErr}
			}
			_, printErr := fmt.Fprintf(a.stdout, "Wrote resolved release values to %s\n", outputPath)
			return printErr
		},
	}
	command.Flags().StringVar(&releaseVersion, "release", "", "Exact semantic release version")
	command.Flags().StringVar(&channel, "channel", "", "Release channel: latest or stable")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Filesystem path or file:// URL containing release documents")
	command.Flags().StringVar(&outputPath, "output", "", "Path for the generated Helm values overlay")
	return command
}

func writeReleaseOverlay(path string, resolved releases.Resolved) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".coyote-release-values-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if overlayErr := releases.WriteValuesOverlay(file, resolved); overlayErr != nil {
		_ = file.Close()
		return overlayErr
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	return os.Rename(temporaryPath, path)
}

func (a *app) newReleasePublishCommand() *cobra.Command {
	var manifestPath, sourceLocation, channel string
	command := &cobra.Command{
		Use:   "publish",
		Short: "Publish an immutable release manifest to a filesystem release source",
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(manifestPath) == "" || strings.TrimSpace(sourceLocation) == "" {
				return &ExitError{Code: 2, Err: errors.New("--manifest and --release-source are required")}
			}
			if channel != "" && channel != "latest" && channel != "stable" {
				return &ExitError{Code: 2, Err: fmt.Errorf("unsupported release channel %q", channel)}
			}
			data, readErr := os.ReadFile(manifestPath)
			if readErr != nil {
				return &ExitError{Code: 1, Err: readErr}
			}
			manifest, parseErr := releases.ParseManifest(data)
			if parseErr != nil {
				return &ExitError{Code: 2, Err: parseErr}
			}
			publisher, publisherErr := releases.NewFilePublisher(sourceLocation)
			if publisherErr != nil {
				return &ExitError{Code: 2, Err: publisherErr}
			}
			digest, publishErr := publisher.PublishManifest(manifest)
			if publishErr != nil {
				return &ExitError{Code: 1, Err: publishErr}
			}
			if channel != "" {
				if channelErr := publisher.PromoteChannel(context.Background(), channel, manifest.Release, digest); channelErr != nil {
					return &ExitError{Code: 1, Err: channelErr}
				}
			}
			_, printErr := fmt.Fprintf(a.stdout, "Published release %s (%s)\n", manifest.Release, digest)
			return printErr
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "Path to a release manifest JSON document")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Filesystem path or file:// URL containing release documents")
	command.Flags().StringVar(&channel, "channel", "", "Optional channel to promote: latest or stable")
	return command
}
