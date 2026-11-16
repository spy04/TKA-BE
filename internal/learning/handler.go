package learning

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"binasiswa/backend/internal/auth"
	"binasiswa/backend/internal/platform/database"
	"binasiswa/backend/internal/platform/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Handler struct {
	service *Service
	auth    *auth.Handler
	logger  *slog.Logger
}

func NewHandler(service *Service, authHandler *auth.Handler, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, auth: authHandler, logger: logger}
}

func (h *Handler) RegisterRoutes(router gin.IRouter) {
	me := router.Group("/me", h.auth.RequireAuthentication(), h.requireStudent())
	me.GET("/overview", h.overview)
	me.GET("/learning/materials/:slug", h.material)
	me.PUT("/lessons/:lessonId/progress", h.saveProgress)
	me.GET("/lessons/:lessonId/notes", h.listNotes)
	me.POST("/lessons/:lessonId/notes", h.createNote)
	me.DELETE("/lessons/:lessonId/notes/:noteId", h.deleteNote)
}

func (h *Handler) requireStudent() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, role, authenticated := auth.PrincipalFromContext(c)
		if !authenticated || role != "STUDENT" {
			h.writeError(c.Writer, http.StatusForbidden, "STUDENT_ACCOUNT_REQUIRED", "A student account is required.")
			c.Abort()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func (h *Handler) overview(c *gin.Context) {
	studentID, _, _ := auth.PrincipalFromContext(c)
	result, err := h.service.Overview(c.Request.Context(), studentID)
	if err != nil {
		h.handleError(c.Writer, "load student learning overview", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) material(c *gin.Context) {
	slug := strings.TrimSpace(c.Param("slug"))
	if slug == "" || len(slug) > 180 {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid material slug is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	result, err := h.service.Material(c.Request.Context(), studentID, slug)
	if err != nil {
		h.handleError(c.Writer, "load student material progress", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) saveProgress(c *gin.Context) {
	lessonID := c.Param("lessonId")
	if !uuidPattern.MatchString(lessonID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid lesson ID is required.")
		return
	}
	var request progressRequest
	if err := httpx.DecodeJSON(c.Writer, c.Request, &request); err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	if request.LastPositionSeconds == nil {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "lastPositionSeconds is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	result, err := h.service.SaveProgress(c.Request.Context(), studentID, lessonID, request)
	if err != nil {
		h.handleError(c.Writer, "save lesson progress", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) listNotes(c *gin.Context) {
	lessonID := c.Param("lessonId")
	if !uuidPattern.MatchString(lessonID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid lesson ID is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	result, err := h.service.Notes(c.Request.Context(), studentID, lessonID)
	if err != nil {
		h.handleError(c.Writer, "list lesson notes", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) createNote(c *gin.Context) {
	lessonID := c.Param("lessonId")
	if !uuidPattern.MatchString(lessonID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid lesson ID is required.")
		return
	}
	var request createNoteRequest
	if err := httpx.DecodeJSON(c.Writer, c.Request, &request); err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	if request.Timestamp == nil {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "timestamp is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	result, err := h.service.CreateNote(c.Request.Context(), studentID, lessonID, request)
	if err != nil {
		h.handleError(c.Writer, "create lesson note", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusCreated, map[string]any{"data": result})
}

func (h *Handler) deleteNote(c *gin.Context) {
	lessonID, noteID := c.Param("lessonId"), c.Param("noteId")
	if !uuidPattern.MatchString(lessonID) || !uuidPattern.MatchString(noteID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "Valid lesson and note IDs are required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	if err := h.service.DeleteNote(c.Request.Context(), studentID, lessonID, noteID); err != nil {
		h.handleError(c.Writer, "delete lesson note", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) handleError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		message := strings.TrimSpace(strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+":"))
		h.writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", message)
	case errors.Is(err, ErrStudentNotFound):
		h.writeError(w, http.StatusNotFound, "STUDENT_PROFILE_NOT_FOUND", "Student profile was not found.")
	case errors.Is(err, ErrMaterialNotFound):
		h.writeError(w, http.StatusNotFound, "MATERIAL_NOT_FOUND", "Accessible published material was not found.")
	case errors.Is(err, ErrLessonNotFound):
		h.writeError(w, http.StatusNotFound, "LESSON_NOT_FOUND", "Accessible published lesson was not found.")
	case errors.Is(err, ErrNoteNotFound):
		h.writeError(w, http.StatusNotFound, "NOTE_NOT_FOUND", "Learning note was not found.")
	case errors.Is(err, ErrServiceUnavailable), database.IsUnavailable(err):
		h.writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is not ready.")
	default:
		h.logger.Error(operation, "error", err)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	if err := httpx.WriteJSON(w, status, value); err != nil {
		h.logger.Error("write learning API response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	if err := httpx.WriteError(w, status, code, message); err != nil {
		h.logger.Error("write learning API error response", "error", err)
	}
}
