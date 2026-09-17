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

// Component is one independently stored portion of a cache preset.
// A preset may retain multiple components for backwards-compatible YAML while
// allowing each portion to have an appropriate reuse key and publish policy.
type Component struct {
	Name             string
	CachePath        string
	FingerprintFiles []string
	KeyDimensions    []string
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

// ResolvePresetComponents expands a user-facing preset into independently
// stored cache components. Single-path presets intentionally retain one
// component named after the preset.
func ResolvePresetComponents(name string, workingDir string) ([]Component, error) {
	preset, err := ResolvePreset(name, workingDir)
	if err != nil {
		return nil, err
	}
	if preset.Name != "go" {
		return []Component{{Name: preset.Name, CachePath: preset.CachePaths[0], FingerprintFiles: preset.FingerprintFiles}}, nil
	}
	return []Component{
		{Name: "go-module", CachePath: "/go/pkg/mod", FingerprintFiles: preset.FingerprintFiles, KeyDimensions: []string{"cache-v2"}},
		{Name: "go-build", CachePath: "/root/.cache/go-build", FingerprintFiles: preset.FingerprintFiles, KeyDimensions: []string{"cache-v2"}},
	}, nil
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
