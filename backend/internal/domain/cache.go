package domain

import "strings"

type CachePolicy string

const (
	CachePolicyPullPush CachePolicy = "pull-push"
	CachePolicyPull     CachePolicy = "pull"
	CachePolicyPush     CachePolicy = "push"
	CachePolicyOff      CachePolicy = "off"
)

// StepCacheConfig is the resolved cache configuration consumed by execution.
// It does not expose storage backend concerns.
type StepCacheConfig struct {
	Preset            string                 `json:"preset,omitempty"`
	Policy            CachePolicy            `json:"policy,omitempty"`
	ComponentPolicies map[string]CachePolicy `json:"component_policies,omitempty"`
}

func (c *StepCacheConfig) Clone() *StepCacheConfig {
	if c == nil {
		return nil
	}
	out := &StepCacheConfig{
		Preset: strings.TrimSpace(c.Preset),
		Policy: c.Policy,
	}
	if len(c.ComponentPolicies) > 0 {
		out.ComponentPolicies = make(map[string]CachePolicy, len(c.ComponentPolicies))
		for component, policy := range c.ComponentPolicies {
			out.ComponentPolicies[component] = NormalizeCachePolicy(policy)
		}
	}
	return out
}

func NormalizeCachePolicy(value CachePolicy) CachePolicy {
	trimmed := strings.ToLower(strings.TrimSpace(string(value)))
	switch CachePolicy(trimmed) {
	case CachePolicyPull, CachePolicyPush, CachePolicyOff, CachePolicyPullPush:
		return CachePolicy(trimmed)
	default:
		return CachePolicyPullPush
	}
}
