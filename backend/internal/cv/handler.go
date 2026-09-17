package cv

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetProfile serves the public Live CV: GET /cv/:slug. Public, no auth —
// anyone with the slug (a shareable handle, not a secret) can view verified
// work. Only non-sensitive user info and anchored entries are exposed.
func (h *Handler) GetProfile(c *gin.Context) {
	slug := c.Param("slug")

	profile, err := h.service.GetProfile(c.Request.Context(), slug)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"profile": profile})
}

// VerifyEntry serves the hash-verification check: GET /cv/:slug/verify/:entryID.
// Public and read-only. If the slug or entry does not resolve, it 404s
// (slug/entry mismatches look identical to "not found") so the endpoint
// never leaks that an entry exists under a different CV.
func (h *Handler) VerifyEntry(c *gin.Context) {
	slug := c.Param("slug")
	entryID := c.Param("entryID")

	result, err := h.service.VerifyEntry(c.Request.Context(), slug, entryID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"verification": result})
}

// RegisterRoutes mounts the public CV endpoints on the root router (no auth).
func RegisterRoutes(router gin.IRouter, h *Handler) {
	router.GET("/cv/:slug", h.GetProfile)
	router.GET("/cv/:slug/verify/:entryID", h.VerifyEntry)
}
