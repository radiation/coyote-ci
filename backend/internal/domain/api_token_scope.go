package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrUnknownAPITokenScope = errors.New("unknown api token scope")

type APITokenScope string

const (
	APITokenScopeBuildRead    APITokenScope = "build:read"
	APITokenScopeBuildLogs    APITokenScope = "build:logs"
	APITokenScopeBuildRun     APITokenScope = "build:run"
	APITokenScopeArtifactRead APITokenScope = "artifact:read"
)

var apiTokenScopeOrder = []APITokenScope{
	APITokenScopeArtifactRead,
	APITokenScopeBuildLogs,
	APITokenScopeBuildRead,
	APITokenScopeBuildRun,
}

var apiTokenScopeSet = map[APITokenScope]struct{}{
	APITokenScopeArtifactRead: {},
	APITokenScopeBuildLogs:    {},
	APITokenScopeBuildRead:    {},
	APITokenScopeBuildRun:     {},
}

func APITokenScopes() []APITokenScope {
	out := make([]APITokenScope, len(apiTokenScopeOrder))
	copy(out, apiTokenScopeOrder)
	return out
}

func NormalizeAPITokenScopes(values []string) ([]APITokenScope, error) {
	if len(values) == 0 {
		return []APITokenScope{}, nil
	}
	seen := make(map[APITokenScope]struct{}, len(values))
	normalized := make([]APITokenScope, 0, len(values))
	for _, value := range values {
		scope := APITokenScope(strings.TrimSpace(value))
		if scope == "" {
			continue
		}
		if _, ok := apiTokenScopeSet[scope]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownAPITokenScope, value)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized, nil
}

func CloneAPITokenScopes(values []APITokenScope) []APITokenScope {
	if len(values) == 0 {
		return []APITokenScope{}
	}
	out := make([]APITokenScope, len(values))
	copy(out, values)
	return out
}

func HasAPITokenScope(values []APITokenScope, scope APITokenScope) bool {
	for _, candidate := range values {
		if candidate == scope {
			return true
		}
	}
	return false
}
