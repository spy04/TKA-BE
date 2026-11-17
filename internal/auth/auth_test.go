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

func TestRefreshCookieAttributes(t *testing.T) {
	tests := []struct {
		name         string
		secure       bool
		wantSecure   bool
		wantSameSite http.SameSite
	}{
		{
			name:         "https cross-site deployment",
			secure:       true,
			wantSecure:   true,
			wantSameSite: http.SameSiteNoneMode,
		},
		{
			name:         "local http development",
			secure:       false,
			wantSecure:   false,
			wantSameSite: http.SameSiteStrictMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configForTest()
			cfg.SecureCookies = tt.secure
			handler := NewHandler(
				NewService(NewRepository(nil), cfg),
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)

			expiresAt := time.Now().Add(time.Hour).UTC()
			setResponse := httptest.NewRecorder()
			handler.setRefreshCookie(setResponse, "refresh-token", expiresAt)
			setCookies := setResponse.Result().Cookies()
			if len(setCookies) != 1 {
				t.Fatalf("set cookie count = %d, want 1", len(setCookies))
			}
			setCookie := setCookies[0]
			assertRefreshCookieAttributes(t, setCookie, tt.wantSecure, tt.wantSameSite)
			if setCookie.Value != "refresh-token" || setCookie.Expires.Unix() != expiresAt.Unix() || setCookie.MaxAge <= 0 {
				t.Fatalf("issued cookie has unexpected value or lifetime: %+v", setCookie)
			}

			clearResponse := httptest.NewRecorder()
			handler.clearRefreshCookie(clearResponse)
			clearCookies := clearResponse.Result().Cookies()
			if len(clearCookies) != 1 {
				t.Fatalf("clear cookie count = %d, want 1", len(clearCookies))
			}
			clearCookie := clearCookies[0]
			assertRefreshCookieAttributes(t, clearCookie, tt.wantSecure, tt.wantSameSite)
			if clearCookie.Value != "" || clearCookie.MaxAge != -1 {
				t.Fatalf("clear cookie does not expire the session: %+v", clearCookie)
			}
		})
	}
}

func assertRefreshCookieAttributes(t *testing.T, cookie *http.Cookie, secure bool, sameSite http.SameSite) {
	t.Helper()
	if cookie.Name != refreshCookieName {
		t.Errorf("cookie name = %q, want %q", cookie.Name, refreshCookieName)
	}
	if cookie.Path != "/api/v1/auth" {
		t.Errorf("cookie path = %q, want /api/v1/auth", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if cookie.Secure != secure {
		t.Errorf("cookie Secure = %t, want %t", cookie.Secure, secure)
	}
	if cookie.SameSite != sameSite {
		t.Errorf("cookie SameSite = %v, want %v", cookie.SameSite, sameSite)
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

func TestIsDuplicateKeyRecognizesPostgresUniqueViolation(t *testing.T) {
	if !isDuplicateKey(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("isDuplicateKey() did not recognize a PostgreSQL unique violation")
	}
	if isDuplicateKey(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("isDuplicateKey() treated a non-unique PostgreSQL error as a duplicate")
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
