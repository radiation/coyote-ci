package memory

import repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"

type BuildStore = repositorymemory.BuildRepository

func NewBuildStore() *BuildStore {
	return repositorymemory.NewBuildRepository()
}
