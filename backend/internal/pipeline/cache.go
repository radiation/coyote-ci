package pipeline

import (
	"strings"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func resolveCache(def *CacheDef) *domain.StepCacheConfig {
	if def == nil {
		return nil
	}

	resolved := &domain.StepCacheConfig{
		Preset: strings.TrimSpace(def.Preset),
		Policy: domain.NormalizeCachePolicy(domain.CachePolicy(def.Policy)),
	}
	components, err := cachepkg.ResolvePresetComponents(resolved.Preset, ".")
	if err != nil {
		return resolved
	}
	resolved.ComponentPolicies = make(map[string]domain.CachePolicy, len(components))
	for _, component := range components {
		policy := resolved.Policy
		if override, ok := def.Components[component.Name]; ok {
			policy = domain.NormalizeCachePolicy(domain.CachePolicy(override))
		}
		resolved.ComponentPolicies[component.Name] = policy
	}
	return resolved
}
