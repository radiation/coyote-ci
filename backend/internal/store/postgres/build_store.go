package postgres

import (
	"database/sql"

	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/store"
)

var _ store.BuildStore = (*BuildStore)(nil)

type BuildStore = repositorypostgres.BuildRepository

func NewBuildStore(db *sql.DB) *BuildStore {
	return repositorypostgres.NewBuildRepository(db)
}
