package dbopen

import (
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
)

func ConfigMode(cfg config.Config) string {
	if cfg.UsesDatabaseURL() {
		return "using DATABASE_URL"
	}

	return "using discrete DB_* settings"
}

func FromConfig(cfg config.Config) (string, platformdb.PoolConfig) {
	return cfg.DatabaseURL(), platformdb.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	}
}
