package deals

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type createArtifactRequest struct {
	Kind       ArtifactKind `json:"kind" binding:"required"`
	StorageKey string       `json:"storage_key" binding:"required"`
}

func (h *Handler) CreateArtifact(c *gin.Context) {
	var req createArtifactRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	artifact := &Artifact{
		DealID:     dealID,
		Kind:       req.Kind,
		StorageKey: req.StorageKey,
	}

	if err := h.service.CreateArtifact(c.Request.Context(), userID, artifact); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"artifact": artifact,
	})
}

func (h *Handler) ListArtifactsByDeal(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	artifacts, err := h.service.ListArtifactsByDeal(c.Request.Context(), userID, dealID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"artifacts": artifacts,
	})
}

func (h *Handler) GetArtifactByID(c *gin.Context) {
	userID := c.GetString("userID")
	artifactID := c.Param("artifactID")

	artifact, err := h.service.GetArtifactByID(c.Request.Context(), userID, artifactID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrArtifactNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"artifact": artifact,
	})
}

func RegisterArtifactRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/deals/:dealID/artifacts")

	group.POST("", h.CreateArtifact)
	group.GET("", h.ListArtifactsByDeal)
	group.GET("/:artifactID", h.GetArtifactByID)
}