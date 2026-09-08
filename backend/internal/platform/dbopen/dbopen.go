package dbopen

import (
	"fmt"

	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
)

func ConfigMode(cfg config.Config) string {
	return "using " + cfg.DatabaseConfigMode()
}

func FromConfig(cfg config.Config) (string, platformdb.PoolConfig, error) {
	databaseURL, err := cfg.DatabaseURL()
	if err != nil {
		return "", platformdb.PoolConfig{}, fmt.Errorf("resolve database URL: %w", err)
	}
	return databaseURL, platformdb.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	}, nil
}
