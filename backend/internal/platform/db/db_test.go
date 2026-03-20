package db

import "testing"

func TestOpen_ErrorPath(t *testing.T) {
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
			t.Parallel()
			db, err := Open(tc.url)
			if err == nil {
				t.Fatalf("expected error, got nil (db=%v)", db)
			}
			if db != nil {
				t.Fatal("expected nil db on error")
			}
		})
	}
}
