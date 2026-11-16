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
)

func TestPasswordHashVerification(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("password hash does not identify Argon2id")
	}
	if !verifyPassword(encoded, password) {
		t.Fatal("verifyPassword() rejected the correct password")
	}
	if verifyPassword(encoded, "incorrect password") {
		t.Fatal("verifyPassword() accepted an incorrect password")
	}
	if verifyPassword("$argon2id$v=19$m=4294967295,t=9,p=1$c2FsdHNhbHQ$YWJj", password) {
		t.Fatal("verifyPassword() accepted an unsafe or malformed hash")
	}
}

func TestTokenIssueAndVerification(t *testing.T) {
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	manager := newTokenManager(strings.Repeat("x", 32), time.Minute)
	manager.now = func() time.Time { return now }

	token, expiresAt, err := manager.issue("user-uuid")
	if err != nil {
		t.Fatalf("issue() error = %v", err)
	}
	if !expiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiresAt = %s, want %s", expiresAt, now.Add(time.Minute))
	}
	claims, err := manager.verify(token)
	if err != nil {
		t.Fatalf("verify() error = %v", err)
	}
	if claims.Subject != "user-uuid" || claims.ExpiresAt != expiresAt.Unix() {
		t.Fatalf("claims = %+v, want the issued subject and expiry", claims)
	}
	if _, err := manager.verify("x" + token[1:]); err == nil {
		t.Fatal("verify() accepted a token with a modified signature")
	}

	now = expiresAt
	if _, err := manager.verify(token); err == nil {
		t.Fatal("verify() accepted an expired token")
	}
}

func TestRefreshTokenStoresOnlyHash(t *testing.T) {
	token, hash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken() error = %v", err)
	}
	if token == "" || len(hash) != 64 || hash == token {
		t.Fatal("refresh token or its SHA-256 hash has an invalid shape")
	}
	if got := hashRefreshToken(token); got != hash {
		t.Fatal("hashRefreshToken() differs from the returned token hash")
	}
}

func TestValidateRegistrationUsesSchemaGradeRanges(t *testing.T) {
	request := registerRequest{
		Email: " Student@Example.com ", Password: "strong passphrase",
		DisplayName: "Student", EducationLevel: "smp", Grade: 7,
	}
	email, level, err := validateRegistration(&request)
	if err != nil {
		t.Fatalf("validateRegistration() error = %v", err)
	}
	if email != "student@example.com" || level != "SMP" {
		t.Fatalf("normalized registration = %q, %q", email, level)
	}

	request.Grade = 10
	if _, _, err := validateRegistration(&request); err == nil {
		t.Fatal("validateRegistration() accepted a grade outside the SMP schema range")
	}
}

func TestRequiredAuthenticationRejectsMissingTokenWithoutDatabase(t *testing.T) {
	service := NewService(NewRepository(nil), configForTest())
	handler := NewHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := gin.New()
	router.GET("/protected", handler.RequireAuthentication(), func(c *gin.Context) {
		t.Fatal("protected handler ran without authentication")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func configForTest() config.AuthConfig {
	return config.AuthConfig{
		AccessTokenSecret: strings.Repeat("x", 32),
		AccessTokenTTL:    time.Minute,
		RefreshTokenTTL:   time.Hour,
	}
}
