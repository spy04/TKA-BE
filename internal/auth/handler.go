package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"binasiswa/backend/internal/platform/database"
	"binasiswa/backend/internal/platform/httpx"
)

const refreshCookieName = "binasiswa_refresh"
const principalContextKey = "auth_principal"

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(router gin.IRouter) {
	router.POST("/auth/register", h.register)
	router.POST("/auth/login", h.login)
	router.POST("/auth/refresh", h.refresh)
	router.POST("/auth/logout", h.logout)
	router.GET("/me", h.RequireAuthentication(), h.me)
}

func (h *Handler) OptionalAuthentication() gin.HandlerFunc {
	return func(c *gin.Context) {
		value := strings.TrimSpace(c.GetHeader("Authorization"))
		if value == "" {
			c.Next()
			return
		}
		identity, ok := h.authenticate(c, value)
		if !ok {
			c.Abort()
			return
		}
		c.Set(principalContextKey, identity)
		c.Next()
	}
}

func (h *Handler) RequireAuthentication() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := h.authenticate(c, c.GetHeader("Authorization"))
		if !ok {
			c.Abort()
			return
		}
		c.Set(principalContextKey, identity)
		c.Next()
	}
}

func PrincipalFromContext(c *gin.Context) (string, string, bool) {
	value, ok := c.Get(principalContextKey)
	identity, isPrincipal := value.(principal)
	return identity.userID, identity.role, ok && isPrincipal
}

func (h *Handler) authenticate(c *gin.Context, authorization string) (principal, bool) {
	fields := strings.Fields(authorization)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		h.writeError(c.Writer, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.")
		return principal{}, false
	}
	identity, err := h.service.Authenticate(c.Request.Context(), fields[1])
	switch {
	case err == nil:
		return identity, true
	case errors.Is(err, ErrInvalidAccessToken):
		h.writeError(c.Writer, http.StatusUnauthorized, "UNAUTHENTICATED", "The access token is invalid or expired.")
	case errors.Is(err, ErrAccountUnavailable):
		h.writeError(c.Writer, http.StatusUnauthorized, "UNAUTHENTICATED", "The account is unavailable.")
	case errors.Is(err, ErrServiceUnavailable):
		h.unavailable(c.Writer)
	default:
		h.databaseError(c.Writer, "authenticate request", err)
	}
	return principal{}, false
}

func (h *Handler) register(c *gin.Context) {
	w, r := c.Writer, c.Request
	w.Header().Set("Cache-Control", "no-store")
	var request registerRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	email, level, err := validateRegistration(&request)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := h.service.Register(r.Context(), request, email, level)
	if err != nil {
		switch {
		case errors.Is(err, ErrDuplicateEmail):
			h.writeError(w, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "An account with this email already exists.")
		case errors.Is(err, ErrServiceUnavailable):
			h.unavailable(w)
		default:
			h.databaseError(w, "register account", err)
		}
		return
	}
	h.setRefreshCookie(w, result.RefreshToken, result.RefreshExpiresAt)
	h.writeJSON(w, http.StatusCreated, map[string]any{
		"data": authResponse{
			User: result.User, AccessToken: result.AccessToken, TokenType: "Bearer",
			ExpiresIn: int64(time.Until(result.AccessExpiresAt).Seconds()),
		},
	})
}

func (h *Handler) login(c *gin.Context) {
	w, r := c.Writer, c.Request
	w.Header().Set("Cache-Control", "no-store")
	var request loginRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	email, err := normalizeEmail(request.Email)
	if err != nil || len(request.Password) == 0 || len(request.Password) > 1024 {
		h.writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "A valid email and password are required.")
		return
	}
	result, err := h.service.Login(r.Context(), email, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			h.writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email or password is incorrect.")
		case errors.Is(err, ErrServiceUnavailable):
			h.unavailable(w)
		default:
			h.databaseError(w, "login", err)
		}
		return
	}
	h.setRefreshCookie(w, result.RefreshToken, result.RefreshExpiresAt)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"data": authResponse{
			User: result.User, AccessToken: result.AccessToken, TokenType: "Bearer",
			ExpiresIn: int64(time.Until(result.AccessExpiresAt).Seconds()),
		},
	})
}

func (h *Handler) refresh(c *gin.Context) {
	w, r := c.Writer, c.Request
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		h.writeError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "A valid refresh session is required.")
		return
	}
	result, err := h.service.Refresh(r.Context(), cookie.Value)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRefreshToken):
			h.writeError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "A valid refresh session is required.")
		case errors.Is(err, ErrServiceUnavailable):
			h.unavailable(w)
		default:
			h.databaseError(w, "refresh authentication session", err)
		}
		return
	}
	h.setRefreshCookie(w, result.RefreshToken, result.RefreshExpiresAt)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"data": accessTokenResponse{
			AccessToken: result.AccessToken, TokenType: "Bearer",
			ExpiresIn: int64(time.Until(result.AccessExpiresAt).Seconds()),
		},
	})
}

func (h *Handler) logout(c *gin.Context) {
	w, r := c.Writer, c.Request
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		h.clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.service.Logout(r.Context(), cookie.Value); err != nil {
		if errors.Is(err, ErrServiceUnavailable) {
			h.unavailable(w)
		} else {
			h.databaseError(w, "revoke authentication session", err)
		}
		return
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(c *gin.Context) {
	w := c.Writer
	w.Header().Set("Cache-Control", "no-store")
	userID, _, ok := PrincipalFromContext(c)
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.")
		return
	}
	user, err := h.service.Me(c.Request.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrAccountUnavailable):
			h.writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "The account is unavailable.")
		case errors.Is(err, ErrServiceUnavailable):
			h.unavailable(w)
		default:
			h.databaseError(w, "load current user", err)
		}
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": user})
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: token, Path: "/api/v1/auth",
		Expires: expiresAt.UTC(), MaxAge: maxAge, HttpOnly: true, Secure: h.service.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: "/api/v1/auth",
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1, HttpOnly: true, Secure: h.service.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	if err := httpx.WriteJSON(w, status, value); err != nil {
		h.logger.Error("write API response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	if err := httpx.WriteError(w, status, code, message); err != nil {
		h.logger.Error("write API error response", "error", err)
	}
}

func (h *Handler) unavailable(w http.ResponseWriter) {
	h.writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is not ready.")
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation, "error", err)
	if database.IsUnavailable(err) {
		h.writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is not ready.")
		return
	}
	h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
}

func validateRegistration(request *registerRequest) (string, string, error) {
	email, err := normalizeEmail(request.Email)
	if err != nil {
		return "", "", errors.New("a valid email is required")
	}
	if !validPassword(request.Password) {
		return "", "", errors.New("password must be at least 12 characters and no more than 1024 bytes")
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 150 {
		return "", "", errors.New("displayName must contain 1 to 150 characters")
	}
	level := strings.ToUpper(strings.TrimSpace(request.EducationLevel))
	minGrade, maxGrade := 0, 0
	switch level {
	case "SD":
		minGrade, maxGrade = 1, 6
	case "SMP":
		minGrade, maxGrade = 7, 9
	case "SMA":
		minGrade, maxGrade = 10, 12
	default:
		return "", "", errors.New("educationLevel must be SD, SMP, or SMA")
	}
	if request.Grade < minGrade || request.Grade > maxGrade {
		return "", "", fmt.Errorf("grade must be between %d and %d for %s", minGrade, maxGrade, level)
	}
	if !validOptionalText(request.School, 200) || !validOptionalText(request.Curriculum, 100) {
		return "", "", errors.New("school or curriculum exceeds the allowed length")
	}
	return email, level, nil
}

func normalizeEmail(value string) (string, error) {
	email := strings.TrimSpace(value)
	if len(email) > 255 || email == "" {
		return "", errors.New("invalid email")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("invalid email")
	}
	return strings.ToLower(email), nil
}

func validOptionalText(value string, maxLength int) bool {
	value = strings.TrimSpace(value)
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxLength
}
