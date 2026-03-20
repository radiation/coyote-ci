package postgres

import (
	"database/sql"

	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
)

type BuildStore = repositorypostgres.BuildRepository

func NewBuildStore(db *sql.DB) *BuildStore {
	return repositorypostgres.NewBuildRepository(db)
}
