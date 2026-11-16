package assessment

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"binasiswa/backend/internal/auth"
	"binasiswa/backend/internal/platform/database"
	"binasiswa/backend/internal/platform/httpx"
)

var assessmentUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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
	api := router.Group("/", h.auth.RequireAuthentication(), h.requireStudent())
	api.POST("/attempts", h.start)
	api.GET("/attempts/:attemptId", h.getAttempt)
	api.PUT("/attempts/:attemptId/answers/:questionId", h.updateAnswer)
	api.POST("/attempts/:attemptId/submit", h.submit)
	api.GET("/attempts/:attemptId/result", h.result)
	api.GET("/me/attempts", h.history)
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

func (h *Handler) start(c *gin.Context) {
	var request startAttemptRequest
	if err := httpx.DecodeJSON(c.Writer, c.Request, &request); err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	request.PackageID = strings.TrimSpace(request.PackageID)
	if !assessmentUUIDPattern.MatchString(request.PackageID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid packageId is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.Start(c.Request.Context(), studentID, request.PackageID)
	if err != nil {
		h.handleError(c, "start assessment attempt", err)
		return
	}
	status := http.StatusCreated
	if response.Resumed {
		status = http.StatusOK
	}
	h.writeJSON(c.Writer, status, map[string]any{"data": response})
}

func (h *Handler) getAttempt(c *gin.Context) {
	attemptID := c.Param("attemptId")
	if !assessmentUUIDPattern.MatchString(attemptID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid attempt ID is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.GetAttempt(c.Request.Context(), studentID, attemptID)
	if err != nil {
		h.handleError(c, "load assessment attempt", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": response})
}

func (h *Handler) updateAnswer(c *gin.Context) {
	attemptID, questionID := c.Param("attemptId"), c.Param("questionId")
	if !assessmentUUIDPattern.MatchString(attemptID) || !assessmentUUIDPattern.MatchString(questionID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "Valid attempt and question IDs are required.")
		return
	}
	var request updateAnswerRequest
	if err := httpx.DecodeJSON(c.Writer, c.Request, &request); err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "INVALID_REQUEST", "The request body must be valid JSON.")
		return
	}
	if request.SelectedOptionKey == nil || !utf8.ValidString(*request.SelectedOptionKey) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "selectedOptionKey is required; use an empty string to clear an answer.")
		return
	}
	selected := strings.TrimSpace(*request.SelectedOptionKey)
	if len(selected) > 10 {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "selectedOptionKey must be at most 10 bytes.")
		return
	}
	if selected == "" {
		request.SelectedOptionKey = nil
	} else {
		request.SelectedOptionKey = &selected
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.UpdateAnswer(c.Request.Context(), studentID, attemptID, questionID, request)
	if err != nil {
		h.handleError(c, "save assessment answer", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": response})
}

func (h *Handler) submit(c *gin.Context) {
	attemptID := c.Param("attemptId")
	if !assessmentUUIDPattern.MatchString(attemptID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid attempt ID is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.Submit(c.Request.Context(), studentID, attemptID)
	if err != nil {
		h.handleError(c, "submit assessment attempt", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": response})
}

func (h *Handler) result(c *gin.Context) {
	attemptID := c.Param("attemptId")
	if !assessmentUUIDPattern.MatchString(attemptID) {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", "A valid attempt ID is required.")
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.Result(c.Request.Context(), studentID, attemptID)
	if err != nil {
		h.handleError(c, "load assessment result", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": response})
}

func (h *Handler) history(c *gin.Context) {
	query, err := parsePageQuery(c)
	if err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	studentID, _, _ := auth.PrincipalFromContext(c)
	response, err := h.service.History(c.Request.Context(), studentID, query)
	if err != nil {
		h.handleError(c, "load assessment history", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": response})
}

func parsePageQuery(c *gin.Context) (pageQuery, error) {
	page := int64(1)
	pageSize := int64(20)
	for key, target := range map[string]*int64{"page": &page, "pageSize": &pageSize} {
		value := strings.TrimSpace(c.Query(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return pageQuery{}, errors.New(key + " must be a positive integer")
		}
		*target = parsed
	}
	if page > 1000000000 || pageSize > 100 {
		return pageQuery{}, errors.New("page must be at most 1000000000 and pageSize must be at most 100")
	}
	return pageQuery{
		Page: page, PageSize: pageSize, Offset: (page - 1) * pageSize,
	}, nil
}

func (h *Handler) handleError(c *gin.Context, operation string, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrOptionNotInQuestion):
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, ErrPackageNotFound), errors.Is(err, ErrAttemptNotFound),
		errors.Is(err, ErrQuestionNotInAttempt):
		h.writeError(c.Writer, http.StatusNotFound, "ASSESSMENT_NOT_FOUND", "The requested assessment resource was not found.")
	case errors.Is(err, ErrAccessDenied):
		h.writeError(c.Writer, http.StatusForbidden, "ASSESSMENT_ACCESS_DENIED", "You do not have access to this assessment.")
	case errors.Is(err, ErrAttemptClosed):
		h.writeError(c.Writer, http.StatusConflict, "ATTEMPT_CLOSED", "This assessment attempt is no longer active.")
	case errors.Is(err, ErrResultNotReady):
		h.writeError(c.Writer, http.StatusConflict, "RESULT_NOT_READY", "The result is available after the attempt is finalized.")
	case errors.Is(err, ErrInvalidPackage):
		h.writeError(c.Writer, http.StatusConflict, "PACKAGE_NOT_READY", "This assessment package is not ready to start.")
	case errors.Is(err, ErrServiceUnavailable), database.IsUnavailable(err):
		h.writeError(c.Writer, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is not ready.")
	default:
		h.logger.Error(operation, "error", err, "request_id", c.GetString("request_id"))
		h.writeError(c.Writer, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	if err := httpx.WriteJSON(w, status, value); err != nil {
		h.logger.Error("write assessment API response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	if err := httpx.WriteError(w, status, code, message); err != nil {
		h.logger.Error("write assessment API error response", "error", err)
	}
}
