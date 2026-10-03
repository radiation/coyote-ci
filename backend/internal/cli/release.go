package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	releases "github.com/radiation/coyote-ci/backend/internal/release"
)

func (a *app) newReleaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "release", Short: "Resolve and publish Coyote software releases"}
	command.AddCommand(a.newReleaseResolveCommand())
	command.AddCommand(a.newReleasePublishCommand())
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
	if err := releases.WriteValuesOverlay(file, resolved); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
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
				if channel != "latest" && channel != "stable" {
					return &ExitError{Code: 2, Err: fmt.Errorf("unsupported release channel %q", channel)}
				}
				index, indexErr := loadChannelIndex(context.Background(), sourceLocation)
				if indexErr != nil {
					return &ExitError{Code: 1, Err: indexErr}
				}
				index.Channels[channel] = releases.ChannelEntry{Release: manifest.Release, ManifestDigest: digest}
				if channelErr := publisher.PublishChannels(index); channelErr != nil {
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

func loadChannelIndex(ctx context.Context, location string) (releases.ChannelIndex, error) {
	source, sourceErr := releases.NewFileSource(location)
	if sourceErr != nil {
		return releases.ChannelIndex{}, sourceErr
	}
	index, indexErr := source.FetchChannelIndex(ctx)
	if errors.Is(indexErr, releases.ErrNotFound) {
		return releases.ChannelIndex{SchemaVersion: releases.SchemaVersion, Channels: map[string]releases.ChannelEntry{}}, nil
	}
	if indexErr != nil {
		return releases.ChannelIndex{}, indexErr
	}
	if validateErr := index.Validate(); validateErr != nil {
		return releases.ChannelIndex{}, validateErr
	}
	return index, nil
}
