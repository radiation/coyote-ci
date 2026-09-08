package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const startupRetryInterval = 500 * time.Millisecond

var startupTimeout = 30 * time.Second

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func Open(databaseURL string, pool PoolConfig) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	return openWithContext(ctx, databaseURL, pool)
}

func openWithContext(ctx context.Context, databaseURL string, pool PoolConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	if err := pingWithRetry(ctx, startupRetryInterval, db.PingContext); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database startup connectivity: %w", err)
	}

	return db, nil
}

func pingWithRetry(ctx context.Context, interval time.Duration, ping func(context.Context) error) error {
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("database did not become ready: %w", lastErr)
			}
			return fmt.Errorf("database startup context ended: %w", err)
		}

		lastErr = ping(ctx)
		if lastErr == nil {
			return nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("database did not become ready: %w", lastErr)
		case <-timer.C:
		}
	}
}
