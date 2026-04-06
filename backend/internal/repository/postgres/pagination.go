package postgres

import "github.com/radiation/coyote-ci/backend/internal/repository"

// clampPageParams delegates to the shared repository.ClampPageParams.
func clampPageParams(p repository.ListParams) (int, int) {
	return repository.ClampPageParams(p)
}
