package postgres

import "github.com/radiation/coyote-ci/backend/internal/repository"

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// clampPageParams returns sanitized limit and offset values. Zero or negative
// limit defaults to defaultPageLimit; values above maxPageLimit are capped.
// Negative offset is clamped to 0.
func clampPageParams(p repository.ListParams) (int, int) {
	limit := p.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}
