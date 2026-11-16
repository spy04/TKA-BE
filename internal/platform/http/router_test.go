package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"binasiswa/backend/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		RequestTimeout: time.Second,
		AllowedOrigins: []string{"http://localhost:5173"},
		Database: config.DatabaseConfig{
			ReadyTimeout: time.Second,
		},
	}
}

func TestHealthDoesNotRequireDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewRouter(logger, nil, testConfig())
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", response.Header().Get("Content-Type"))
	}
}

func TestReadyReportsUnconfiguredDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewRouter(logger, nil, testConfig())
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Body.String(); !strings.Contains(got, "not_configured") {
		t.Fatalf("body = %q, want database not_configured status", got)
	}
}

func TestCORSAllowsConfiguredOriginOnly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewRouter(logger, nil, testConfig())

	allowed := httptest.NewRequest(http.MethodGet, "/health", nil)
	allowed.Header.Set("Origin", "http://localhost:5173")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowed)
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allowed origin header = %q", got)
	}
	if got := allowedResponse.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credential header = %q, want true", got)
	}

	blocked := httptest.NewRequest(http.MethodGet, "/health", nil)
	blocked.Header.Set("Origin", "https://example.com")
	blockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != http.StatusForbidden {
		t.Fatalf("blocked origin status = %d, want %d", blockedResponse.Code, http.StatusForbidden)
	}
}

func TestAPIEndpointsAreMounted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewRouter(logger, nil, testConfig())

	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/materials?grade=invalid", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/materials/test-slug", "", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/v1/assessment-packages?kind=invalid", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/auth/register", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/auth/login", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/auth/refresh", "", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/auth/logout", "", http.StatusNoContent},
		{http.MethodGet, "/api/v1/me", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/overview", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/learning/materials/test-slug", "", http.StatusUnauthorized},
		{http.MethodPut, "/api/v1/me/lessons/123e4567-e89b-12d3-a456-426614174000/progress", `{}`, http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/lessons/123e4567-e89b-12d3-a456-426614174000/notes", "", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/me/lessons/123e4567-e89b-12d3-a456-426614174000/notes", `{}`, http.StatusUnauthorized},
		{http.MethodDelete, "/api/v1/me/lessons/123e4567-e89b-12d3-a456-426614174000/notes/123e4567-e89b-12d3-a456-426614174001", "", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/attempts", `{}`, http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/attempts/123e4567-e89b-12d3-a456-426614174000", "", http.StatusUnauthorized},
		{http.MethodPut, "/api/v1/attempts/123e4567-e89b-12d3-a456-426614174000/answers/123e4567-e89b-12d3-a456-426614174001", `{}`, http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/attempts/123e4567-e89b-12d3-a456-426614174000/submit", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/attempts/123e4567-e89b-12d3-a456-426614174000/result", "", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/me/attempts", "", http.StatusUnauthorized},
		{http.MethodGet, "/health", "", http.StatusOK},
		{http.MethodGet, "/ready", "", http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
