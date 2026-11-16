package catalog

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"binasiswa/backend/internal/auth"
	"binasiswa/backend/internal/platform/database"
	"binasiswa/backend/internal/platform/httpx"
)

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
	router.GET("/materials", h.listMaterials)
	router.GET("/materials/:slug", h.auth.OptionalAuthentication(), h.getMaterial)
	router.GET("/assessment-packages", h.listPackages)
}

func (h *Handler) listMaterials(c *gin.Context) {
	query, err := parseListQuery(c.Request.URL.Query(), false)
	if err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := h.service.ListMaterials(c.Request.Context(), query)
	if err != nil {
		h.databaseError(c.Writer, "list published materials", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) getMaterial(c *gin.Context) {
	w := c.Writer
	if !h.service.Available() {
		h.unavailable(w)
		return
	}
	slug := strings.TrimSpace(c.Param("slug"))
	if slug == "" || len(slug) > 180 {
		h.writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "A valid material slug is required.")
		return
	}
	userID, role, authenticated := auth.PrincipalFromContext(c)
	result, err := h.service.GetMaterial(c.Request.Context(), slug, userID, role, authenticated)
	switch {
	case err == nil:
		h.writeJSON(w, http.StatusOK, map[string]any{"data": result})
	case errors.Is(err, ErrMaterialNotFound):
		h.writeError(w, http.StatusNotFound, "MATERIAL_NOT_FOUND", "Published material was not found.")
	case errors.Is(err, ErrPremiumAccessRequired):
		h.writeError(w, http.StatusForbidden, "PREMIUM_ACCESS_REQUIRED", "Access to this material is not available.")
	default:
		h.databaseError(w, "load published material", err)
	}
}

func (h *Handler) listPackages(c *gin.Context) {
	query, err := parseListQuery(c.Request.URL.Query(), true)
	if err != nil {
		h.writeError(c.Writer, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := h.service.ListPackages(c.Request.Context(), query)
	if err != nil {
		h.databaseError(c.Writer, "list published assessment packages", err)
		return
	}
	h.writeJSON(c.Writer, http.StatusOK, map[string]any{"data": result})
}

func parseListQuery(values url.Values, packages bool) (listQuery, error) {
	query := listQuery{Page: 1, PageSize: 20}
	var err error
	if query.Page, err = parseInt64Alias(values, []string{"page", "halaman"}, 1, 1, 1_000_000); err != nil {
		return listQuery{}, errors.New("page must be a positive integer no greater than 1000000")
	}
	if query.PageSize, err = parseInt64Alias(values, []string{"pageSize", "perPage", "limit", "batas"}, 20, 1, 100); err != nil {
		return listQuery{}, errors.New("pageSize must be between 1 and 100")
	}
	query.Offset = (query.Page - 1) * query.PageSize
	query.Search = strings.TrimSpace(firstValue(values, "q", "search", "cari"))
	if utf8.RuneCountInString(query.Search) > 100 {
		return listQuery{}, errors.New("search must be no more than 100 characters")
	}
	query.Level = strings.ToUpper(strings.TrimSpace(firstValue(values, "educationLevel", "level", "jenjang")))
	if query.Level == "SEMUA" {
		query.Level = ""
	}
	if query.Level != "" && query.Level != "SD" && query.Level != "SMP" && query.Level != "SMA" {
		return listQuery{}, errors.New("educationLevel must be SD, SMP, or SMA")
	}
	gradeValue := strings.TrimSpace(firstValue(values, "grade", "kelas"))
	if gradeValue != "" && strings.ToLower(gradeValue) != "semua" {
		grade, parseErr := strconv.Atoi(gradeValue)
		if parseErr != nil || grade < 1 || grade > 12 {
			return listQuery{}, errors.New("grade must be an integer between 1 and 12")
		}
		query.Grade = grade
	}
	query.Subject = strings.TrimSpace(firstValue(values, "subject", "mataPelajaran", "mata-pelajaran"))
	if strings.EqualFold(query.Subject, "semua") {
		query.Subject = ""
	}
	if len(query.Subject) > 150 {
		return listQuery{}, errors.New("subject must be no more than 150 characters")
	}
	query.Access = strings.ToUpper(strings.TrimSpace(firstValue(values, "accessTier", "access", "akses")))
	switch query.Access {
	case "GRATIS":
		query.Access = "FREE"
	case "SEMUA":
		query.Access = ""
	case "", "FREE", "PREMIUM":
	default:
		return listQuery{}, errors.New("accessTier must be FREE or PREMIUM")
	}
	query.Sort = strings.ToLower(strings.TrimSpace(firstValue(values, "sort", "urutan")))
	if packages {
		query.Kind = strings.ToUpper(strings.TrimSpace(firstValue(values, "kind", "jenis")))
		if query.Kind == "SEMUA" {
			query.Kind = ""
		}
		if query.Kind != "" && query.Kind != "PRACTICE" && query.Kind != "SIMULATION" {
			return listQuery{}, errors.New("kind must be PRACTICE or SIMULATION")
		}
		query.Difficulty = strings.ToUpper(strings.TrimSpace(firstValue(values, "difficulty", "tingkat")))
		switch query.Difficulty {
		case "MUDAH":
			query.Difficulty = "EASY"
		case "SEDANG":
			query.Difficulty = "MEDIUM"
		case "SULIT":
			query.Difficulty = "HARD"
		case "SEMUA":
			query.Difficulty = ""
		}
		if query.Difficulty != "" && query.Difficulty != "EASY" && query.Difficulty != "MEDIUM" && query.Difficulty != "HARD" {
			return listQuery{}, errors.New("difficulty must be EASY, MEDIUM, or HARD")
		}
		if query.Sort != "" && query.Sort != "terbaru" && query.Sort != "newest" && query.Sort != "soal" && query.Sort != "questions" {
			return listQuery{}, errors.New("sort must be terbaru or soal")
		}
	} else if query.Sort != "" && query.Sort != "relevan" && query.Sort != "terbaru" && query.Sort != "populer" && query.Sort != "latihan" {
		return listQuery{}, errors.New("sort must be relevan, terbaru, populer, or latihan")
	}
	return query, nil
}

func parseInt64Alias(values url.Values, names []string, defaultValue, min, max int64) (int64, error) {
	raw := strings.TrimSpace(firstValue(values, names...))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < min || value > max {
		return 0, errors.New("out of range")
	}
	return value, nil
}

func firstValue(values url.Values, names ...string) string {
	for _, name := range names {
		if values.Has(name) {
			return values.Get(name)
		}
	}
	return ""
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	if err := httpx.WriteJSON(w, status, value); err != nil {
		h.logger.Error("write catalog API response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	if err := httpx.WriteError(w, status, code, message); err != nil {
		h.logger.Error("write catalog API error response", "error", err)
	}
}

func (h *Handler) unavailable(w http.ResponseWriter) {
	h.writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is not ready.")
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation, "error", err)
	if errors.Is(err, ErrServiceUnavailable) || database.IsUnavailable(err) {
		h.unavailable(w)
		return
	}
	h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
}
