package domain

import "strings"

type CacheScope string

const (
	CacheScopeBuild CacheScope = "build"
	CacheScopeJob   CacheScope = "job"
)

// StepCacheConfig is the resolved cache configuration consumed by execution.
// It does not expose storage backend concerns.
type StepCacheConfig struct {
	Preset   string     `json:"preset,omitempty"`
	Scope    CacheScope `json:"scope"`
	Paths    []string   `json:"paths"`
	KeyFiles []string   `json:"key_files"`
}

func (c *StepCacheConfig) Clone() *StepCacheConfig {
	if c == nil {
		return nil
	}
	out := &StepCacheConfig{
		Preset: strings.TrimSpace(c.Preset),
		Scope:  c.Scope,
	}
	out.Paths = append([]string(nil), c.Paths...)
	out.KeyFiles = append([]string(nil), c.KeyFiles...)
	return out
}
