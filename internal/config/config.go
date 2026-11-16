package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address         string
	Environment     string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	AllowedOrigins  []string
	Database        DatabaseConfig
	Auth            AuthConfig
}

type AuthConfig struct {
	AccessTokenSecret string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	SecureCookies     bool
}

type DatabaseConfig struct {
	Enabled            bool
	MaxOpenConnections int
	MaxIdleConnections int
	ConnectionLifetime time.Duration
	ConnectionIdleTime time.Duration
	ReadyTimeout       time.Duration
	URL                string
}

func Load() (Config, error) {
	environment, err := loadDevelopmentEnv()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Address:         envString("APP_ADDR", ":8080"),
		Environment:     environment,
		RequestTimeout:  30 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}

	if cfg.RequestTimeout, err = envDuration("APP_REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("APP_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}

	cfg.AllowedOrigins, err = parseOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}

	cfg.Database, err = loadDatabaseConfig()
	if err != nil {
		return Config{}, err
	}

	cfg.Auth, err = loadAuthConfig(environment)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadAuthConfig(environment string) (AuthConfig, error) {
	auth := AuthConfig{
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		SecureCookies:   !strings.EqualFold(environment, "development"),
	}
	var err error
	if auth.AccessTokenTTL, err = envDuration("AUTH_ACCESS_TOKEN_TTL", auth.AccessTokenTTL); err != nil {
		return AuthConfig{}, err
	}
	if auth.RefreshTokenTTL, err = envDuration("AUTH_REFRESH_TOKEN_TTL", auth.RefreshTokenTTL); err != nil {
		return AuthConfig{}, err
	}
	if auth.RefreshTokenTTL <= auth.AccessTokenTTL {
		return AuthConfig{}, fmt.Errorf("AUTH_REFRESH_TOKEN_TTL must exceed AUTH_ACCESS_TOKEN_TTL")
	}

	auth.AccessTokenSecret = os.Getenv("AUTH_ACCESS_TOKEN_SECRET")
	if auth.AccessTokenSecret == "" && !strings.EqualFold(environment, "development") {
		return AuthConfig{}, fmt.Errorf("AUTH_ACCESS_TOKEN_SECRET is required outside development")
	}
	if auth.AccessTokenSecret == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return AuthConfig{}, fmt.Errorf("generate development access token secret: %w", err)
		}
		auth.AccessTokenSecret = base64.RawURLEncoding.EncodeToString(secret)
	} else if len(auth.AccessTokenSecret) < 32 {
		return AuthConfig{}, fmt.Errorf("AUTH_ACCESS_TOKEN_SECRET must contain at least 32 bytes")
	}

	return auth, nil
}

func loadDevelopmentEnv() (string, error) {
	environment, configured := os.LookupEnv("APP_ENV")
	if configured {
		environment = strings.TrimSpace(environment)
		if environment == "" {
			environment = "development"
		}
		if !strings.EqualFold(environment, "development") {
			return environment, nil
		}
	}

	envPath, err := findEnvFile()
	if err != nil {
		return "", err
	}

	values := map[string]string{}
	if envPath != "" {
		values, err = readEnvFile(envPath)
		if err != nil {
			return "", err
		}
	}
	if !configured {
		if fileEnvironment, exists := values["APP_ENV"]; exists {
			environment = strings.TrimSpace(fileEnvironment)
			if environment == "" {
				environment = "development"
			}
		} else {
			environment = "development"
		}
	}

	if !strings.EqualFold(environment, "development") {
		return environment, nil
	}
	return applyDevelopmentEnv(values, environment)
}

func applyDevelopmentEnv(values map[string]string, environment string) (string, error) {
	if !strings.EqualFold(environment, "development") {
		return environment, nil
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return "", fmt.Errorf("set environment variable %s from .env: %w", key, err)
		}
	}
	return environment, nil
}

func findEnvFile() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory for .env: %w", err)
	}

	candidates := []string{
		filepath.Join(workingDirectory, ".env"),
		filepath.Join(workingDirectory, "backend", ".env"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect development .env file: %w", err)
		}
	}
	return "", nil
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open development .env file: %w", err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		separator := strings.IndexByte(line, '=')
		if separator <= 0 {
			return nil, fmt.Errorf("invalid .env syntax at line %d", lineNumber)
		}

		key := strings.TrimSpace(line[:separator])
		if !validEnvKey(key) {
			return nil, fmt.Errorf("invalid .env variable name at line %d", lineNumber)
		}
		value := strings.TrimSpace(line[separator+1:])
		if len(value) > 0 && (value[0] == '"' || value[0] == '\'') {
			quote := value[0]
			if len(value) < 2 || value[len(value)-1] != quote {
				return nil, fmt.Errorf("invalid quoted .env value at line %d", lineNumber)
			}
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read development .env file: %w", err)
	}
	return values, nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, char := range key {
		if (char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			char == '_' ||
			(i > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func loadDatabaseConfig() (DatabaseConfig, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		for _, key := range []string{"DB_HOST", "DB_USER", "DB_NAME", "DB_PASSWORD"} {
			if strings.TrimSpace(os.Getenv(key)) != "" {
				return DatabaseConfig{}, fmt.Errorf("DATABASE_URL is required; DB_* connection settings are not supported")
			}
		}
		return DatabaseConfig{}, nil
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil || (parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql") ||
		parsedURL.Hostname() == "" || parsedURL.Path == "" {
		return DatabaseConfig{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL connection URL")
	}

	db := DatabaseConfig{
		Enabled:            true,
		MaxOpenConnections: 25,
		MaxIdleConnections: 5,
		ConnectionLifetime: 5 * time.Minute,
		ConnectionIdleTime: time.Minute,
		ReadyTimeout:       2 * time.Second,
		URL:                databaseURL,
	}
	if db.MaxOpenConnections, err = envInt("DB_MAX_OPEN_CONNS", db.MaxOpenConnections); err != nil {
		return DatabaseConfig{}, err
	}
	if db.MaxIdleConnections, err = envInt("DB_MAX_IDLE_CONNS", db.MaxIdleConnections); err != nil {
		return DatabaseConfig{}, err
	}
	if db.ConnectionLifetime, err = envDuration("DB_CONN_MAX_LIFETIME", db.ConnectionLifetime); err != nil {
		return DatabaseConfig{}, err
	}
	if db.ConnectionIdleTime, err = envDuration("DB_CONN_MAX_IDLE_TIME", db.ConnectionIdleTime); err != nil {
		return DatabaseConfig{}, err
	}
	if db.ReadyTimeout, err = envDuration("DB_READY_TIMEOUT", db.ReadyTimeout); err != nil {
		return DatabaseConfig{}, err
	}

	if db.MaxOpenConnections < 1 {
		return DatabaseConfig{}, fmt.Errorf("DB_MAX_OPEN_CONNS must be greater than zero")
	}
	if db.MaxIdleConnections < 0 || db.MaxIdleConnections > db.MaxOpenConnections {
		return DatabaseConfig{}, fmt.Errorf("DB_MAX_IDLE_CONNS must be between zero and DB_MAX_OPEN_CONNS")
	}
	if db.ConnectionLifetime <= 0 || db.ConnectionIdleTime <= 0 || db.ReadyTimeout <= 0 {
		return DatabaseConfig{}, fmt.Errorf("database connection and readiness durations must be greater than zero")
	}

	return db, nil
}

func parseOrigins(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	origins := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(origins))
	for i := range origins {
		origin := strings.TrimSpace(origins[i])
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			(parsed.Path != "" && parsed.Path != "/") ||
			parsed.RawQuery != "" || parsed.Fragment != "" ||
			parsed.User != nil || origin == "*" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS contains an invalid origin %q", origin)
		}
		origin = strings.TrimSuffix(origin, "/")
		if _, exists := seen[origin]; exists {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS contains duplicate origin %q", origin)
		}
		seen[origin] = struct{}{}
		origins[i] = origin
	}
	return origins, nil
}

func envString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return parsed, nil
}

