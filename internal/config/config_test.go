package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearDatabaseEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DB_HOST",
		"DB_PORT",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_LIFETIME",
		"DB_CONN_MAX_IDLE_TIME",
		"DB_READY_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadAllowsDatabaseToBeUnconfigured(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Enabled {
		t.Fatal("database should be disabled when connection settings are empty")
	}
}

func TestLoadRejectsPartialDatabaseConfiguration(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("DB_HOST", "localhost")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want partial database configuration error")
	}
}

func TestLoadRejectsWildcardCORSOrigin(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid CORS origin error")
	}
}

func TestLoadDevelopmentEnvPreservesExistingEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADDR", ":9090")
	t.Setenv("DB_USER", "")
	t.Setenv("ENV_TEST_PASSWORD", "")
	if err := os.Unsetenv("ENV_TEST_PASSWORD"); err != nil {
		t.Fatalf("unset test environment variable: %v", err)
	}
	t.Setenv("ENV_TEST_NEW", "")
	if err := os.Unsetenv("ENV_TEST_NEW"); err != nil {
		t.Fatalf("unset test environment variable: %v", err)
	}

	values := map[string]string{
		"APP_ENV":           "development",
		"APP_ADDR":          ":8080",
		"DB_USER":           "from-file",
		"ENV_TEST_PASSWORD": "file-secret",
		"DB_NAME":           "db_binasiswa",
		"ENV_TEST_NEW":      "loaded",
	}

	environment, err := applyDevelopmentEnv(values, "development")
	if err != nil {
		t.Fatalf("applyDevelopmentEnv() error = %v", err)
	}
	if environment != "development" {
		t.Fatalf("environment = %q, want development", environment)
	}
	if got := os.Getenv("APP_ADDR"); got != ":9090" {
		t.Fatalf("APP_ADDR = %q, want existing environment value", got)
	}
	if got := os.Getenv("DB_USER"); got != "" {
		t.Fatalf("DB_USER = %q, want existing empty environment value", got)
	}
	if got := os.Getenv("ENV_TEST_PASSWORD"); got != "file-secret" {
		t.Fatal("new development env variable was not loaded")
	}
	if got := os.Getenv("ENV_TEST_NEW"); got != "loaded" {
		t.Fatalf("new development env variable = %q, want loaded", got)
	}
}

func TestLoadDevelopmentEnvSkipsFileOutsideDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("ENV_TEST_PRODUCTION", "")

	environment, err := applyDevelopmentEnv(map[string]string{
		"APP_ENV":             "development",
		"ENV_TEST_PRODUCTION": "must-not-load",
	}, "production")
	if err != nil {
		t.Fatalf("applyDevelopmentEnv() error = %v", err)
	}
	if environment != "production" {
		t.Fatalf("environment = %q, want production", environment)
	}
	if got := os.Getenv("ENV_TEST_PRODUCTION"); got != "" {
		t.Fatalf("production loaded development .env value %q", got)
	}
}

func TestReadEnvFileSupportsQuotedValuesAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# local config\nAPP_ENV=development\nDB_PASSWORD='value=with#characters'\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temporary env file: %v", err)
	}

	values, err := readEnvFile(path)
	if err != nil {
		t.Fatalf("readEnvFile() error = %v", err)
	}
	if got := values["DB_PASSWORD"]; got != "value=with#characters" {
		t.Fatalf("quoted password value was parsed incorrectly")
	}
}
