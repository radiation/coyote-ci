package pipeline

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func resolveCache(def *CacheDef) *domain.StepCacheConfig {
	if def == nil {
		return nil
	}

	return &domain.StepCacheConfig{
		Preset: strings.TrimSpace(def.Preset),
		Policy: domain.NormalizeCachePolicy(domain.CachePolicy(def.Policy)),
	}
}
