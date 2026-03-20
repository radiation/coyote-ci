package memory

import (
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/store"
)

var _ store.BuildStore = (*BuildStore)(nil)

type BuildStore = repositorymemory.BuildRepository

func NewBuildStore() *BuildStore {
	return repositorymemory.NewBuildRepository()
}
