package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/cli/output"
	releases "github.com/radiation/coyote-ci/backend/internal/release"
)

const releaseFetchNotFoundExitCode = 3

func (a *app) newReleaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "release", Short: "Resolve and publish Coyote software releases"}
	command.AddCommand(a.newReleaseResolveCommand())
	command.AddCommand(a.newReleasePublishCommand())
	command.AddCommand(a.newReleasePromoteChannelCommand())
	command.AddCommand(a.newReleasePublishCoyoteCommand())
	command.AddCommand(a.newReleaseFetchCommand())
	command.AddCommand(a.newReleaseImportCommand())
	command.AddCommand(a.newApplicationReleaseShowCommand())
	command.AddCommand(a.newApplicationReleasePromoteCommand())
	return command
}

func (a *app) newReleasePromoteChannelCommand() *cobra.Command {
	var releaseVersion, channel, sourceLocation string
	command := &cobra.Command{
		Use:   "promote-channel",
		Short: "Promote an existing immutable release to a channel",
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
			releaseVersion = strings.TrimSpace(releaseVersion)
			channel = strings.TrimSpace(channel)
			sourceLocation = strings.TrimSpace(sourceLocation)
			if releaseVersion == "" || channel == "" || sourceLocation == "" {
				return &ExitError{Code: 2, Err: errors.New("--release, --channel, and --release-source are required")}
			}
			if versionErr := releases.ValidateExactSemanticVersion(releaseVersion); versionErr != nil {
				return &ExitError{Code: 2, Err: fmt.Errorf("--release %w", versionErr)}
			}
			if channel != "latest" && channel != "stable" {
				return &ExitError{Code: 2, Err: fmt.Errorf("unsupported release channel %q", channel)}
			}
			store, closer, storeErr := a.openReleaseStore(cmd.Context(), sourceLocation)
			if storeErr != nil {
				return releaseStoreOpenExitError(storeErr)
			}
			defer closeReleaseStore(&commandErr, closer)
			document, fetchErr := store.FetchManifest(cmd.Context(), releaseVersion)
			if fetchErr != nil {
				if errors.Is(fetchErr, releases.ErrNotFound) {
					return &ExitError{Code: releaseFetchNotFoundExitCode, Err: fetchErr}
				}
				return &ExitError{Code: 1, Err: fetchErr}
			}
			manifest, parseErr := releases.ParseManifest(document.Data)
			if parseErr != nil {
				return &ExitError{Code: 1, Err: parseErr}
			}
			if manifest.Release != releaseVersion {
				return &ExitError{Code: 1, Err: fmt.Errorf("release source returned manifest %q for requested release %q", manifest.Release, releaseVersion)}
			}
			digest := releases.Digest(document.Data)
			promotion, promoteErr := store.PromoteChannel(cmd.Context(), channel, releaseVersion, digest)
			if promoteErr != nil {
				return &ExitError{Code: 1, Err: promoteErr}
			}
			previousTarget := "<unknown>"
			if promotion.PreviousKnown && promotion.Previous == nil {
				previousTarget = "<none>"
			} else if promotion.Previous != nil {
				previousTarget = fmt.Sprintf("%s (%s)", promotion.Previous.Release, promotion.Previous.ManifestDigest)
			}
			changed := "unknown"
			if promotion.ChangedKnown {
				changed = fmt.Sprintf("%t", promotion.Changed)
			}
			_, printErr := fmt.Fprintf(a.stdout, "Channel: %s\nPrevious target: %s\nNew target: %s (%s)\nManifest digest: %s\nChanged: %s\n", channel, previousTarget, promotion.Current.Release, promotion.Current.ManifestDigest, digest, changed)
			return printErr
		},
	}
	command.Flags().StringVar(&releaseVersion, "release", "", "Exact semantic release version")
	command.Flags().StringVar(&channel, "channel", "", "Release channel: latest or stable")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
	return command
}

func (a *app) newReleasePublishCoyoteCommand() *cobra.Command {
	var buildID, sourceLocation, channel, helmRepository string
	var immutableChartTags bool
	command := &cobra.Command{
		Use:   "publish-coyote",
		Short: "Publish a Coyote product release from a complete generic application release",
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
			if strings.TrimSpace(buildID) == "" || strings.TrimSpace(sourceLocation) == "" || strings.TrimSpace(helmRepository) == "" {
				return &ExitError{Code: 2, Err: errors.New("--build, --release-source and --helm-repository are required")}
			}
			if !immutableChartTags {
				return &ExitError{Code: 2, Err: errors.New("--immutable-chart-tags-confirmed is required after verifying registry policy")}
			}
			if channel != "" && channel != "latest" && channel != "stable" {
				return &ExitError{Code: 2, Err: fmt.Errorf("unsupported release channel %q", channel)}
			}
			target, err := a.resolveTarget()
			if err != nil {
				return err
			}
			client, err := a.newClient(target)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			genericRelease, err := client.GetBuildApplicationRelease(cmd.Context(), buildID)
			if err != nil {
				return mapCommandError(err)
			}
			build, err := client.GetBuild(cmd.Context(), buildID)
			if err != nil {
				return mapCommandError(err)
			}
			sourceCommit := build.SourceSHA
			if sourceCommit == nil {
				sourceCommit = build.SourceCommitSHA
			}
			if build.ApplicationVersion == nil || sourceCommit == nil {
				return &ExitError{Code: 1, Err: errors.New("build must have an application version and source commit")}
			}
			if genericRelease.ApplicationVersion != *build.ApplicationVersion {
				return &ExitError{Code: 1, Err: errors.New("generic release version does not match build application version")}
			}
			if build.ID != buildID || genericRelease.BuildID != buildID || build.Status != "success" || genericRelease.Status != "complete" {
				return &ExitError{Code: 1, Err: errors.New("a successful build and its complete application release are required")}
			}
			directory, err := os.MkdirTemp("", "coyote-release-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(directory) }()
			listed, err := client.ListBuildArtifacts(cmd.Context(), buildID)
			if err != nil {
				return mapCommandError(err)
			}
			artifacts := make(map[string]releases.CoyoteArtifact)
			outputs := make([]releases.CoyoteReleaseOutput, 0, len(genericRelease.Outputs))
			for _, output := range genericRelease.Outputs {
				if output.Status != "published" {
					return &ExitError{Code: 1, Err: fmt.Errorf("output %s is not published", output.Key)}
				}
				if output.Kind == "artifact" {
					name := strings.TrimPrefix(output.Key, "artifact:")
					if output.ArtifactID == nil || artifacts[name].Path != "" {
						return &ExitError{Code: 1, Err: errors.New("missing or duplicate release artifact")}
					}
					for _, artifact := range listed.Artifacts {
						if artifact.ID != *output.ArtifactID {
							continue
						}
						if artifact.BuildID != buildID || artifact.ChecksumSHA256 == nil {
							return &ExitError{Code: 1, Err: errors.New("artifact build identity or checksum missing")}
						}
						file, createErr := os.CreateTemp(directory, "artifact-")
						if createErr != nil {
							return createErr
						}
						downloadErr := client.DownloadBuildArtifact(cmd.Context(), buildID, artifact.ID, file)
						closeErr := file.Close()
						if downloadErr != nil || closeErr != nil {
							return errors.Join(downloadErr, closeErr)
						}
						location, locationErr := url.JoinPath(target.ServerURL, "api", "builds", buildID, "artifacts", artifact.ID, "download")
						if locationErr != nil {
							return locationErr
						}
						artifacts[name] = releases.CoyoteArtifact{Path: file.Name(), URL: location, SHA256: *artifact.ChecksumSHA256, BuildID: artifact.BuildID, Size: artifact.SizeBytes}
					}
					continue
				}
				outputs = append(outputs, releases.CoyoteReleaseOutput{
					Key: output.Key, Kind: output.Kind, Status: output.Status,
					TargetImageReference: output.TargetImageReference, ImmutableReference: output.ImmutableReference,
				})
			}
			input, err := releases.VerifyCoyoteArtifacts(releases.CoyoteReleaseInput{
				BuildID: buildID,
				Status:  genericRelease.Status, ApplicationVersion: *build.ApplicationVersion,
				SourceCommit: *sourceCommit, CreatedAt: build.CreatedAt, Outputs: outputs,
			}, artifacts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if migrationErr := releases.VerifyCoyoteMigrationTarget(cmd.Context(), input); migrationErr != nil {
				return &ExitError{Code: 1, Err: migrationErr}
			}
			input.Helm, err = releases.PublishCoyoteChart(cmd.Context(), artifacts["helm-chart"].Path, helmRepository, input.ApplicationVersion)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			manifest, err := releases.CoyoteManifest(input)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			publisher, closer, err := a.openReleaseStore(cmd.Context(), sourceLocation)
			if err != nil {
				return releaseStoreOpenExitError(err)
			}
			defer closeReleaseStore(&commandErr, closer)
			digest, err := publisher.PublishManifest(cmd.Context(), manifest)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			switch channel {
			case "latest":
				if _, latestErr := publisher.PromoteCoyoteLatestIfNewer(cmd.Context(), manifest.Release, digest); latestErr != nil {
					return &ExitError{Code: 1, Err: latestErr}
				}
			case "stable":
				if _, channelErr := publisher.PromoteChannel(cmd.Context(), channel, manifest.Release, digest); channelErr != nil {
					return &ExitError{Code: 1, Err: channelErr}
				}
			}
			return output.Write(target.OutputMode, a.stdout, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Published Coyote release %s (%s)\n", manifest.Release, digest)
				return err
			}, map[string]string{"release": manifest.Release, "manifest_digest": digest})
		},
	}
	command.Flags().StringVar(&buildID, "build", "", "Build ID with a complete generic application release")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
	command.Flags().StringVar(&helmRepository, "helm-repository", "", "Immutable OCI repository ending in /coyote-ci")
	command.Flags().BoolVar(&immutableChartTags, "immutable-chart-tags-confirmed", false, "Confirm the chart repository enforces immutable version tags")
	command.Flags().StringVar(&channel, "channel", "", "Optional product channel to promote: latest or stable")
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
		RunE: func(cmd *cobra.Command, args []string) (commandErr error) {
			if (strings.TrimSpace(releaseVersion) == "") == (strings.TrimSpace(channel) == "") {
				return &ExitError{Code: 2, Err: errors.New("exactly one of --release or --channel is required")}
			}
			if strings.TrimSpace(sourceLocation) == "" || strings.TrimSpace(outputPath) == "" {
				return &ExitError{Code: 2, Err: errors.New("--release-source and --output are required")}
			}
			source, closer, sourceErr := a.openReleaseStore(cmd.Context(), sourceLocation)
			if sourceErr != nil {
				return releaseStoreOpenExitError(sourceErr)
			}
			defer closeReleaseStore(&commandErr, closer)
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
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
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
		Short: "Publish an immutable release manifest to a release store",
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
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
			publisher, closer, publisherErr := a.openReleaseStore(cmd.Context(), sourceLocation)
			if publisherErr != nil {
				return releaseStoreOpenExitError(publisherErr)
			}
			defer closeReleaseStore(&commandErr, closer)
			digest, publishErr := publisher.PublishManifest(cmd.Context(), manifest)
			if publishErr != nil {
				return &ExitError{Code: 1, Err: publishErr}
			}
			if channel != "" {
				if _, channelErr := publisher.PromoteChannel(cmd.Context(), channel, manifest.Release, digest); channelErr != nil {
					return &ExitError{Code: 1, Err: channelErr}
				}
			}
			_, printErr := fmt.Fprintf(a.stdout, "Published release %s (%s)\n", manifest.Release, digest)
			return printErr
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "Path to a release manifest JSON document")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
	command.Flags().StringVar(&channel, "channel", "", "Optional channel to promote: latest or stable")
	return command
}

func (a *app) newReleaseFetchCommand() *cobra.Command {
	var releaseVersion, sourceLocation, outputPath string
	command := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch the exact stored release manifest bytes",
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
			if strings.TrimSpace(releaseVersion) == "" || strings.TrimSpace(sourceLocation) == "" || strings.TrimSpace(outputPath) == "" {
				return &ExitError{Code: 2, Err: errors.New("--version, --release-source, and --output are required")}
			}
			if versionErr := releases.ValidateExactSemanticVersion(releaseVersion); versionErr != nil {
				return &ExitError{Code: 2, Err: fmt.Errorf("--version %w", versionErr)}
			}
			store, closer, storeErr := a.openReleaseStore(cmd.Context(), sourceLocation)
			if storeErr != nil {
				return releaseStoreOpenExitError(storeErr)
			}
			defer closeReleaseStore(&commandErr, closer)
			document, fetchErr := store.FetchManifest(cmd.Context(), releaseVersion)
			if fetchErr != nil {
				if errors.Is(fetchErr, releases.ErrNotFound) {
					return &ExitError{Code: releaseFetchNotFoundExitCode, Err: fetchErr}
				}
				return &ExitError{Code: 1, Err: fetchErr}
			}
			if outputPath == "-" {
				_, writeErr := a.stdout.Write(document.Data)
				return writeErr
			}
			if writeErr := writeRawReleaseDocument(outputPath, document.Data); writeErr != nil {
				return &ExitError{Code: 1, Err: writeErr}
			}
			_, printErr := fmt.Fprintf(a.stdout, "Wrote release manifest %s to %s\n", releaseVersion, outputPath)
			return printErr
		},
	}
	command.Flags().StringVar(&releaseVersion, "version", "", "Exact semantic release version")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
	command.Flags().StringVar(&outputPath, "output", "", "Output path, or - for raw stdout")
	return command
}

func (a *app) newReleaseImportCommand() *cobra.Command {
	var manifestPath, sourceLocation, expectedDigest string
	command := &cobra.Command{
		Use:   "import",
		Short: "Import an existing release manifest without reserializing it",
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
			if strings.TrimSpace(manifestPath) == "" || strings.TrimSpace(sourceLocation) == "" || strings.TrimSpace(expectedDigest) == "" {
				return &ExitError{Code: 2, Err: errors.New("--file, --release-source, and --expected-digest are required")}
			}
			data, readErr := os.ReadFile(manifestPath)
			if readErr != nil {
				return &ExitError{Code: 1, Err: readErr}
			}
			manifest, parseErr := releases.ParseManifest(data)
			if parseErr != nil {
				return &ExitError{Code: 2, Err: parseErr}
			}
			if releases.Digest(data) != strings.TrimSpace(expectedDigest) {
				return &ExitError{Code: 2, Err: errors.New("--expected-digest does not match manifest bytes")}
			}
			store, closer, storeErr := a.openReleaseStore(cmd.Context(), sourceLocation)
			if storeErr != nil {
				return releaseStoreOpenExitError(storeErr)
			}
			defer closeReleaseStore(&commandErr, closer)
			_, digest, importErr := store.ImportManifest(cmd.Context(), data)
			if importErr != nil {
				return &ExitError{Code: 1, Err: importErr}
			}
			_, printErr := fmt.Fprintf(a.stdout, "Imported release %s (%s)\n", manifest.Release, digest)
			return printErr
		},
	}
	command.Flags().StringVar(&manifestPath, "file", "", "Existing release manifest JSON document")
	command.Flags().StringVar(&sourceLocation, "release-source", "", "Release store path, file:// URL, or gs:// location")
	command.Flags().StringVar(&expectedDigest, "expected-digest", "", "SHA-256 digest of the exact manifest bytes")
	return command
}

func closeReleaseStore(commandErr *error, closer io.Closer) {
	if closer == nil {
		return
	}
	if closeErr := closer.Close(); closeErr != nil && *commandErr == nil {
		*commandErr = &ExitError{Code: 1, Err: closeErr}
	}
}

func releaseStoreOpenExitError(err error) *ExitError {
	if errors.Is(err, releases.ErrInvalidLocation) || errors.Is(err, releases.ErrUnsupportedBackend) {
		return &ExitError{Code: 2, Err: err}
	}
	return &ExitError{Code: 1, Err: err}
}

func writeRawReleaseDocument(path string, data []byte) error {
	parent := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(parent, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	file, createErr := os.CreateTemp(parent, ".coyote-release-manifest-*")
	if createErr != nil {
		return createErr
	}
	temporaryPath := file.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if _, writeErr := file.Write(data); writeErr != nil {
		_ = file.Close()
		return writeErr
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	return os.Rename(temporaryPath, path)
}
