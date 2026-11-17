package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"binasiswa/backend/internal/assessment"
	"binasiswa/backend/internal/auth"
	"binasiswa/backend/internal/catalog"
	"binasiswa/backend/internal/config"
	"binasiswa/backend/internal/learning"
	"github.com/gin-gonic/gin"
)

const requestIDContextKey = "request_id"

func NewRouter(logger *slog.Logger, db *sql.DB, cfg config.Config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true

	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		origins[origin] = struct{}{}
	}

	router.Use(
		requestID(logger),
		requestLogger(logger),
		recoverer(logger),
		cors(origins),
		requestTimeout(cfg.RequestTimeout),
	)
	router.GET("/health", func(c *gin.Context) {
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	router.GET("/ready", readyHandler(logger, db, cfg.Database.ReadyTimeout))

	api := router.Group("/api/v1")
	authRepository := auth.NewRepository(db)
	authService := auth.NewService(authRepository, cfg.Auth)
	authHandler := auth.NewHandler(authService, logger)
	authHandler.RegisterRoutes(api)
	catalogRepository := catalog.NewRepository(db)
	catalogService := catalog.NewService(catalogRepository)
	catalog.NewHandler(catalogService, authHandler, logger).RegisterRoutes(api)
	learningRepository := learning.NewRepository(db)
	learningService := learning.NewService(learningRepository)
	learning.NewHandler(learningService, authHandler, logger).RegisterRoutes(api)
	assessmentRepository := assessment.NewRepository(db)
	assessmentService := assessment.NewService(assessmentRepository)
	assessment.NewHandler(assessmentService, authHandler, logger).RegisterRoutes(api)

	return http.TimeoutHandler(router, cfg.RequestTimeout, `{"error":{"code":"REQUEST_TIMEOUT","message":"request timed out"}}`)
}

func readyHandler(logger *slog.Logger, db *sql.DB, timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			writeJSON(c.Writer, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"checks": map[string]string{"database": "not_configured"},
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			logger.WarnContext(c.Request.Context(), "database readiness check failed",
				"request_id", c.GetString(requestIDContextKey),
				"error", err,
			)
			writeJSON(c.Writer, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"checks": map[string]string{"database": "unavailable"},
			})
			return
		}

		writeJSON(c.Writer, http.StatusOK, map[string]any{
			"status": "ready",
			"checks": map[string]string{"database": "ok"},
		})
	}
}

func requestID(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			logger.ErrorContext(c.Request.Context(), "generate request ID", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, map[string]any{
				"error": map[string]string{
					"code":    "INTERNAL_ERROR",
					"message": "internal server error",
				},
			})
			return
		}
		id := hex.EncodeToString(value[:])
		c.Set(requestIDContextKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.InfoContext(c.Request.Context(), "http request",
			"request_id", c.GetString(requestIDContextKey),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"bytes", c.Writer.Size(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func recoverer(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(c.Request.Context(), "panic recovered",
					"request_id", c.GetString(requestIDContextKey),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, map[string]any{
						"error": map[string]string{
							"code":    "INTERNAL_ERROR",
							"message": "internal server error",
						},
					})
					return
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func cors(allowedOrigins map[string]struct{}) gin.HandlerFunc {
	const allowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	const allowedHeaders = "Accept, Authorization, Content-Type, X-Request-ID"

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}
		if _, allowed := allowedOrigins[origin]; !allowed {
			c.String(http.StatusForbidden, "origin not allowed")
			c.Abort()
			return
		}

		header := c.Writer.Header()
		header.Add("Vary", "Origin")
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Allow-Methods", allowedMethods)
		header.Set("Access-Control-Allow-Headers", allowedHeaders)
		header.Set("Access-Control-Max-Age", "600")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func requestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
