package pipeline

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

const cachePresetGolang = "golang"

func resolveCache(def *CacheDef) *domain.StepCacheConfig {
	if def == nil {
		return nil
	}

	resolved := &domain.StepCacheConfig{
		Preset: strings.TrimSpace(def.Preset),
		Scope:  domain.CacheScope(strings.TrimSpace(def.Scope)),
	}

	presetPaths, presetKeyFiles := cachePresetDefaults(resolved.Preset)

	if len(def.Paths) > 0 {
		resolved.Paths = cloneStringSlice(def.Paths)
	} else {
		resolved.Paths = cloneStringSlice(presetPaths)
	}

	if len(def.Key.Files) > 0 {
		resolved.KeyFiles = cloneStringSlice(def.Key.Files)
	} else {
		resolved.KeyFiles = cloneStringSlice(presetKeyFiles)
	}

	return resolved
}

func cachePresetDefaults(name string) (paths []string, keyFiles []string) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case cachePresetGolang:
		// These defaults follow official golang container image conventions.
		// Non-root or custom images may use different cache directories.
		return []string{"/go/pkg/mod", "/root/.cache/go-build"}, []string{"go.mod", "go.sum"}
	default:
		return nil, nil
	}
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		cloned = append(cloned, trimmed)
	}
	return cloned
}
