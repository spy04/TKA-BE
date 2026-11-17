package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"binasiswa/backend/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPasswordHashVerification(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") || !verifyPassword(encoded, password) || verifyPassword(encoded, "incorrect password") {
		t.Fatal("password verification did not preserve the Argon2id contract")
	}
}

func TestTokenIssueAndVerificationRejectsExpiryAndTampering(t *testing.T) {
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	manager := newTokenManager(strings.Repeat("x", 32), time.Minute)
	manager.now = func() time.Time { return now }
	token, expiresAt, err := manager.issue("user-uuid")
	if err != nil {
		t.Fatalf("issue() error = %v", err)
	}
	if claims, err := manager.verify(token); err != nil || claims.Subject != "user-uuid" || claims.ExpiresAt != expiresAt.Unix() {
		t.Fatalf("verify() = %+v, %v", claims, err)
	}
	if _, err := manager.verify("x" + token[1:]); err == nil {
		t.Fatal("verify() accepted a modified token")
	}
	now = expiresAt
	if _, err := manager.verify(token); err == nil {
		t.Fatal("verify() accepted an expired token")
	}
}

func TestLogoutIsStatelessAndDoesNotSetCookies(t *testing.T) {
	handler := NewHandler(NewService(NewRepository(nil), configForTest()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := gin.New()
	handler.RegisterRoutes(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("logout set cookies: %+v", cookies)
	}
}

func TestRequiredAuthenticationRejectsMissingToken(t *testing.T) {
	handler := NewHandler(NewService(NewRepository(nil), configForTest()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := gin.New()
	router.GET("/protected", handler.RequireAuthentication(), func(c *gin.Context) { t.Fatal("protected handler ran") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestValidateRegistrationUsesSchemaGradeRanges(t *testing.T) {
	request := registerRequest{Email: " Student@Example.com ", Password: "strong passphrase", DisplayName: "Student", EducationLevel: "smp", Grade: 7}
	email, level, err := validateRegistration(&request)
	if err != nil || email != "student@example.com" || level != "SMP" {
		t.Fatalf("registration validation = %q, %q, %v", email, level, err)
	}
	request.Grade = 10
	if _, _, err := validateRegistration(&request); err == nil {
		t.Fatal("accepted a grade outside the SMP range")
	}
}

func TestIsDuplicateKeyRecognizesPostgresUniqueViolation(t *testing.T) {
	if !isDuplicateKey(&pgconn.PgError{Code: "23505"}) || isDuplicateKey(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("PostgreSQL unique violation classification is incorrect")
	}
}

func configForTest() config.AuthConfig {
	return config.AuthConfig{AccessTokenSecret: strings.Repeat("x", 32), AccessTokenTTL: time.Minute}
}
