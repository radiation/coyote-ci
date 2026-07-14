package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/apiclient"
	"github.com/radiation/coyote-ci/backend/internal/cli/atomicfile"
	"github.com/radiation/coyote-ci/backend/internal/cli/output"
)

var replaceFileAtomicFunc = atomicfile.ReplaceFileAtomic

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
