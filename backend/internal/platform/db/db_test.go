package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpen_ErrorPath(t *testing.T) {
	originalStartupTimeout := startupTimeout
	startupTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		startupTimeout = originalStartupTimeout
	})

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "invalid host fails ping",
			url:  "postgres://user:pass@127.0.0.1:1/coyote_ci?sslmode=disable&connect_timeout=1",
		},
		{
			name: "malformed url fails",
			url:  "://bad-url",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(tc.url, PoolConfig{
				MaxOpenConns:    10,
				MaxIdleConns:    5,
				ConnMaxLifetime: 30 * time.Minute,
				ConnMaxIdleTime: 5 * time.Minute,
			})
			if err == nil {
				t.Fatalf("expected error, got nil (db=%v)", db)
			}
			if db != nil {
				t.Fatal("expected nil db on error")
			}
		})
	}
}

func TestPingWithRetry(t *testing.T) {
	t.Run("immediate success", func(t *testing.T) {
		calls := 0
		retryErr := pingWithRetry(context.Background(), 0, func(context.Context) error {
			calls++
			return nil
		})
		if retryErr != nil || calls != 1 {
			t.Fatalf("retry error=%v calls=%d", retryErr, calls)
		}
	})

	t.Run("transient failure then success", func(t *testing.T) {
		calls := 0
		retryErr := pingWithRetry(context.Background(), 0, func(context.Context) error {
			calls++
			if calls == 1 {
				return errors.New("proxy not ready")
			}
			return nil
		})
		if retryErr != nil || calls != 2 {
			t.Fatalf("retry error=%v calls=%d", retryErr, calls)
		}
	})

	t.Run("retry exhaustion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		retryErr := pingWithRetry(ctx, 0, func(context.Context) error {
			calls++
			cancel()
			return errors.New("proxy not ready")
		})
		if retryErr == nil || calls != 1 || !strings.Contains(retryErr.Error(), "proxy not ready") {
			t.Fatalf("retry error=%v calls=%d", retryErr, calls)
		}
	})

	t.Run("context cancellation before first attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		retryErr := pingWithRetry(ctx, 0, func(context.Context) error {
			calls++
			return nil
		})
		if retryErr == nil || calls != 0 || !strings.Contains(retryErr.Error(), "context canceled") {
			t.Fatalf("retry error=%v calls=%d", retryErr, calls)
		}
	})
}
