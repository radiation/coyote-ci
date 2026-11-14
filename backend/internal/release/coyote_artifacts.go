package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type CoyoteArtifact struct {
	Path, URL, SHA256, BuildID string
	Size                       int64
}

func VerifyCoyoteArtifacts(input CoyoteReleaseInput, artifacts map[string]CoyoteArtifact) (CoyoteReleaseInput, error) {
	if len(artifacts) != len(CoyoteCLIPlatforms)+5 {
		return input, errors.New("release requires six CLI archives, checksums, CLI metadata, chart, compatibility and schema target")
	}
	for name, artifact := range artifacts {
		checksum, size, readErr := artifactChecksum(artifact.Path)
		if readErr != nil {
			return input, readErr
		}
		if artifact.BuildID != input.BuildID || size <= 0 || size != artifact.Size || checksum != artifact.SHA256 {
			return input, fmt.Errorf("artifact %s failed build, size or checksum verification", name)
		}
	}
	read := func(name string) ([]byte, error) {
		artifact, found := artifacts[name]
		if !found || artifact.Size > 1024*1024 {
			return nil, fmt.Errorf("missing or oversized release metadata %s", name)
		}
		return os.ReadFile(artifact.Path)
	}
	checksums, err := read("cli-checksums")
	if err != nil {
		return input, err
	}
	expected := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(checksums)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !sha256Pattern.MatchString(fields[0]) || filepath.Base(fields[1]) != fields[1] || expected[fields[1]] != "" {
			return input, errors.New("invalid or duplicate CLI checksum entry")
		}
		expected[fields[1]] = fields[0]
	}
	input.CLIArtifacts = make(map[string]CLIArtifact)
	for _, platform := range CoyoteCLIPlatforms {
		artifact, found := artifacts["cli-"+platform]
		extension := ".tar.gz"
		if strings.HasPrefix(platform, "windows-") {
			extension = ".zip"
		}
		filename := "coyote_" + strings.ReplaceAll(platform, "-", "_") + extension
		if !found || expected[filename] != artifact.SHA256 {
			return input, fmt.Errorf("missing or mismatched CLI checksum for %s", platform)
		}
		input.CLIArtifacts[platform] = CLIArtifact{URL: artifact.URL, Filename: filename, Size: artifact.Size, SHA256: artifact.SHA256, BuildID: artifact.BuildID}
	}
	metadata, err := read("cli-metadata")
	if err != nil {
		return input, err
	}
	var identity struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if parseErr := json.Unmarshal(metadata, &identity); parseErr != nil || identity.Version != input.ApplicationVersion || identity.Commit != input.SourceCommit {
		return input, errors.New("CLI metadata does not match the release version and source commit")
	}
	compatibility, err := read("compatibility")
	if err != nil {
		return input, err
	}
	input.Compatibility = &Compatibility{}
	decoder := json.NewDecoder(strings.NewReader(string(compatibility)))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(input.Compatibility); decodeErr != nil {
		return input, decodeErr
	}
	if decodeErr := decoder.Decode(new(any)); decodeErr != io.EOF {
		return input, errors.New("unexpected trailing compatibility data")
	}
	if compatibilityErr := validateCompatibility(input.Compatibility); compatibilityErr != nil {
		return input, compatibilityErr
	}
	schema, err := read("schema-target")
	if err != nil {
		return input, err
	}
	if strings.TrimSpace(string(schema)) != input.Compatibility.SchemaTarget {
		return input, errors.New("compatibility schema_target differs from bundled Goose target")
	}
	if cliErr := validateCLIArtifacts(input.CLIArtifacts); cliErr != nil {
		return input, cliErr
	}
	return input, verifyCoyoteChart(artifacts["helm-chart"].Path, input.ApplicationVersion)
}

func VerifyCoyoteMigrationTarget(ctx context.Context, input CoyoteReleaseInput) error {
	if compatibilityErr := validateCompatibility(input.Compatibility); compatibilityErr != nil {
		return compatibilityErr
	}
	for _, output := range input.Outputs {
		if output.Key != CoyoteMigrateOutputKey {
			continue
		}
		if imageErr := ValidateImageReference(output.ImmutableReference); imageErr != nil {
			return imageErr
		}
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			command := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--platform", platform, output.ImmutableReference, "target")
			target, runErr := command.Output()
			if runErr != nil {
				return fmt.Errorf("cannot verify %s migration image target: %w", platform, runErr)
			}
			if strings.TrimSpace(string(target)) != input.Compatibility.SchemaTarget {
				return fmt.Errorf("%s migration image target differs from compatibility schema_target", platform)
			}
		}
		return nil
	}
	return errors.New("missing migration image")
}

func artifactChecksum(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

func verifyCoyoteChart(path, version string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = compressed.Close() }()
	archive := tar.NewReader(compressed)
	for {
		header, nextErr := archive.Next()
		if nextErr != nil {
			return fmt.Errorf("chart metadata missing: %w", nextErr)
		}
		if header.Name != "coyote-ci/Chart.yaml" {
			continue
		}
		if header.Size > 1024*1024 {
			return errors.New("oversized chart metadata")
		}
		var chart struct {
			Name       string `yaml:"name"`
			Version    string `yaml:"version"`
			AppVersion string `yaml:"appVersion"`
		}
		if decodeErr := yaml.NewDecoder(archive).Decode(&chart); decodeErr != nil {
			return decodeErr
		}
		if chart.Name != "coyote-ci" || chart.Version != version || chart.AppVersion != version {
			return errors.New("chart name, version and appVersion must match the application release")
		}
		return nil
	}
}

func PublishCoyoteChart(ctx context.Context, archive, repository, version string) (*HelmPackage, error) {
	if chartErr := verifyCoyoteChart(archive, version); chartErr != nil {
		return nil, chartErr
	}
	if !strings.HasPrefix(repository, "oci://") || !strings.HasSuffix(repository, "/coyote-ci") || strings.ContainsAny(repository, "? #\n\r") {
		return nil, errors.New("helm repository must be an OCI reference ending in /coyote-ci")
	}
	directory, err := os.MkdirTemp("", "coyote-chart-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	want, _, err := artifactChecksum(archive)
	if err != nil {
		return nil, err
	}
	tagReference := strings.TrimPrefix(repository, "oci://") + ":" + strings.ReplaceAll(version, "+", "_")
	pull := func() ([]byte, error) {
		return exec.CommandContext(ctx, "helm", "pull", repository, "--version", version, "--destination", directory).CombinedOutput()
	}
	output, pullErr := pull()
	if pullErr != nil {
		message := strings.ToLower(string(output))
		missing := strings.Contains(message, "manifest unknown") || strings.HasSuffix(strings.TrimSpace(message), strings.ToLower(tagReference)+": not found")
		if !missing || strings.Contains(message, "denied") || strings.Contains(message, "unauthorized") {
			return nil, fmt.Errorf("cannot verify chart availability: %w: %s", pullErr, output)
		}
		_, pushErr := exec.CommandContext(ctx, "helm", "push", archive, strings.TrimSuffix(repository, "/coyote-ci")).CombinedOutput()
		_, pullErr = pull()
		if pullErr != nil {
			return nil, fmt.Errorf("cannot verify chart publication: %w", errors.Join(pushErr, pullErr))
		}
	}
	pulledChart := filepath.Join(directory, "coyote-ci-"+version+".tgz")
	actual, _, err := artifactChecksum(pulledChart)
	if err != nil {
		return nil, err
	}
	if actual != want {
		return nil, errors.New("immutable chart version contains different bytes")
	}
	descriptorJSON, inspectErr := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect", "--format", "{{json .Manifest}}", tagReference).Output()
	if inspectErr != nil {
		return nil, fmt.Errorf("resolving chart OCI descriptor: %w", inspectErr)
	}
	var descriptor struct {
		Digest string `json:"digest"`
	}
	if decodeErr := json.Unmarshal(descriptorJSON, &descriptor); decodeErr != nil {
		return nil, fmt.Errorf("decoding chart OCI descriptor: %w", decodeErr)
	}
	if !strings.HasPrefix(descriptor.Digest, "sha256:") || !sha256Pattern.MatchString(strings.TrimPrefix(descriptor.Digest, "sha256:")) {
		return nil, errors.New("chart OCI descriptor must contain a SHA-256 digest")
	}
	if removeErr := os.Remove(pulledChart); removeErr != nil {
		return nil, removeErr
	}
	if _, pinnedErr := exec.CommandContext(ctx, "helm", "pull", repository+"@"+descriptor.Digest, "--version", version, "--destination", directory).CombinedOutput(); pinnedErr != nil {
		return nil, fmt.Errorf("verifying digest-pinned chart: %w", pinnedErr)
	}
	archives, globErr := filepath.Glob(filepath.Join(directory, "*.tgz"))
	if globErr != nil || len(archives) != 1 {
		return nil, errors.New("digest-pinned pull must produce exactly one chart archive")
	}
	actual, _, err = artifactChecksum(archives[0])
	if err != nil {
		return nil, err
	}
	if actual != want {
		return nil, errors.New("digest-pinned chart contains different bytes")
	}
	return &HelmPackage{Name: "coyote-ci", Repository: repository, Version: version, Digest: descriptor.Digest}, nil
}
