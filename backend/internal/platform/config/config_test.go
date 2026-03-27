package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		expected  Config
		useNoEnvs bool
	}{
		{
			name: "uses defaults when env is unset",
			env: map[string]string{
				"APP_PORT":                  "",
				"DB_HOST":                   "",
				"DB_PORT":                   "",
				"DB_USER":                   "",
				"DB_PASSWORD":               "",
				"DB_NAME":                   "",
				"DB_SSLMODE":                "",
				"WORKER_STEP_LEASE_SECONDS": "",
			},
			expected: Config{
				AppPort:          "8080",
				DBHost:           "localhost",
				DBPort:           "5432",
				DBUser:           "coyote",
				DBPassword:       "coyote",
				DBName:           "coyote_ci",
				DBSSLMode:        "disable",
				StepLeaseSeconds: 45,
			},
		},
		{
			name: "uses env values when set",
			env: map[string]string{
				"APP_PORT":                  "9999",
				"DB_HOST":                   "db.internal",
				"DB_PORT":                   "5433",
				"DB_USER":                   "user1",
				"DB_PASSWORD":               "pass1",
				"DB_NAME":                   "name1",
				"DB_SSLMODE":                "require",
				"WORKER_STEP_LEASE_SECONDS": "60",
			},
			expected: Config{
				AppPort:          "9999",
				DBHost:           "db.internal",
				DBPort:           "5433",
				DBUser:           "user1",
				DBPassword:       "pass1",
				DBName:           "name1",
				DBSSLMode:        "require",
				StepLeaseSeconds: 60,
			},
		},
		{
			name: "invalid lease seconds falls back to default",
			env: map[string]string{
				"APP_PORT":                  "",
				"DB_HOST":                   "",
				"DB_PORT":                   "",
				"DB_USER":                   "",
				"DB_PASSWORD":               "",
				"DB_NAME":                   "",
				"DB_SSLMODE":                "",
				"WORKER_STEP_LEASE_SECONDS": "not-an-int",
			},
			expected: Config{
				AppPort:          "8080",
				DBHost:           "localhost",
				DBPort:           "5432",
				DBUser:           "coyote",
				DBPassword:       "coyote",
				DBName:           "coyote_ci",
				DBSSLMode:        "disable",
				StepLeaseSeconds: 45,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got := Load()
			if got != tc.expected {
				t.Fatalf("expected %+v, got %+v", tc.expected, got)
			}
		})
	}
}

func TestConfig_DatabaseURL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		expected string
	}{
		{
			name: "builds url from config fields",
			cfg: Config{
				DBUser:     "user",
				DBPassword: "pass",
				DBHost:     "localhost",
				DBPort:     "5432",
				DBName:     "db",
				DBSSLMode:  "disable",
			},
			expected: "postgres://user:pass@localhost:5432/db?sslmode=disable",
		},
		{
			name: "keeps provided ssl mode",
			cfg: Config{
				DBUser:     "u",
				DBPassword: "p",
				DBHost:     "h",
				DBPort:     "1",
				DBName:     "n",
				DBSSLMode:  "require",
			},
			expected: "postgres://u:p@h:1/n?sslmode=require",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.cfg.DatabaseURL()
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
