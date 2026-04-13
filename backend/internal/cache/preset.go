package cache

import (
	"errors"
	"path"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUnknownPreset = errors.New("unknown cache preset")

type Preset struct {
	Name             string
	CachePaths       []string
	FingerprintFiles []string
}

func ResolvePreset(name string, workingDir string) (Preset, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	baseDir := normalizeWorkingDir(workingDir)

	switch normalized {
	case "node":
		return Preset{
			Name:             "node",
			CachePaths:       []string{"/root/.npm"},
			FingerprintFiles: []string{path.Join(baseDir, "package-lock.json")},
		}, nil
	case "python-uv":
		return Preset{
			Name:             "python-uv",
			CachePaths:       []string{"/root/.cache/uv"},
			FingerprintFiles: []string{path.Join(baseDir, "uv.lock")},
		}, nil
	case "go":
		return Preset{
			Name:             "go",
			CachePaths:       []string{"/go/pkg/mod", "/root/.cache/go-build"},
			FingerprintFiles: []string{path.Join(baseDir, "go.sum")},
		}, nil
	default:
		return Preset{}, ErrUnknownPreset
	}
}

func SupportedPresets() []string {
	return []string{"node", "python-uv", "go"}
}

func IsSupportedPreset(name string) bool {
	_, err := ResolvePreset(name, ".")
	return err == nil
}

func IsSupportedPolicy(value string) bool {
	switch domain.NormalizeCachePolicy(domain.CachePolicy(value)) {
	case domain.CachePolicyPullPush, domain.CachePolicyPull, domain.CachePolicyPush, domain.CachePolicyOff:
		trimmed := strings.ToLower(strings.TrimSpace(value))
		return trimmed == "" || trimmed == string(domain.CachePolicyPullPush) || trimmed == string(domain.CachePolicyPull) || trimmed == string(domain.CachePolicyPush) || trimmed == string(domain.CachePolicyOff)
	default:
		return false
	}
}

func normalizeWorkingDir(workingDir string) string {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" || trimmed == "." {
		return "."
	}
	cleaned := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	if cleaned == "" {
		return "."
	}
	return cleaned
}
