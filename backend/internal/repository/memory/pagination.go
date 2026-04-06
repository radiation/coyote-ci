package memory

import "github.com/radiation/coyote-ci/backend/internal/repository"

// clampMemoryPageParams delegates to the shared repository.ClampPageParams.
func clampMemoryPageParams(p repository.ListParams) (int, int) {
	return repository.ClampPageParams(p)
}
