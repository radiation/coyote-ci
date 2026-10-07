package versioning

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

const maxApplicationVersionFileSize = 4096

var applicationVersionSemVerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:(?:0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:(?:0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

type ApplicationVersionConfig struct {
	Value    string
	Template string
	File     string
}

func (c ApplicationVersionConfig) Empty() bool {
	return strings.TrimSpace(c.Value) == "" && strings.TrimSpace(c.Template) == "" && strings.TrimSpace(c.File) == ""
}

func ValidateApplicationVersionConfig(config ApplicationVersionConfig) error {
	sources := 0
	if strings.TrimSpace(config.Value) != "" {
		sources++
	}
	if strings.TrimSpace(config.Template) != "" {
		sources++
	}
	if strings.TrimSpace(config.File) != "" {
		sources++
	}
	if sources > 1 {
		return fmt.Errorf("exactly one of value, template, or file may be configured")
	}
	if sources == 0 {
		return nil
	}
	if strings.TrimSpace(config.Value) != "" {
		return ValidateApplicationVersion(config.Value)
	}
	if strings.TrimSpace(config.Template) != "" {
		return ValidateArtifactVersionTemplate(config.Template)
	}
	return validateApplicationVersionFilePath(config.File)
}

func ResolveApplicationVersion(config ApplicationVersionConfig, build domain.Build) (string, error) {
	if err := ValidateApplicationVersionConfig(config); err != nil {
		return "", err
	}
	if strings.TrimSpace(config.Value) != "" {
		return normalizeApplicationVersion(config.Value)
	}
	if strings.TrimSpace(config.Template) == "" {
		return "", nil
	}
	resolved, err := ResolveArtifactVersionTemplate(config.Template, build)
	if err != nil {
		return "", err
	}
	return normalizeApplicationVersion(resolved)
}

func ReadApplicationVersionFile(workspaceRoot string, configuredPath string) (string, error) {
	if err := validateApplicationVersionFilePath(configuredPath); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(strings.TrimSpace(configuredPath)))
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("read application version file: %w", err)
	}
	relative, err := filepath.Rel(root, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("application version file must remain within the prepared workspace")
	}
	file, err := os.Open(resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("open application version file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxApplicationVersionFileSize+1))
	if err != nil {
		return "", fmt.Errorf("read application version file: %w", err)
	}
	if len(data) > maxApplicationVersionFileSize {
		return "", fmt.Errorf("application version file exceeds %d bytes", maxApplicationVersionFileSize)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("application version file must be valid UTF-8")
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("application version file is empty")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("application version file must contain one line")
	}
	return normalizeApplicationVersion(value)
}

func ValidateApplicationVersion(value string) error {
	_, err := normalizeApplicationVersion(value)
	return err
}

func normalizeApplicationVersion(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !applicationVersionSemVerPattern.MatchString(trimmed) {
		return "", fmt.Errorf("application version must be SemVer without build metadata: %q", trimmed)
	}
	return trimmed, nil
}

func validateApplicationVersionFilePath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("application version file is required")
	}
	if filepath.IsAbs(trimmed) {
		return fmt.Errorf("application version file must be workspace-relative")
	}
	cleaned := pathClean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("application version file must be workspace-relative and must not traverse parent directories")
	}
	return nil
}

func pathClean(value string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
}
