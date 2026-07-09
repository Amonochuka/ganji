package deals

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type createVerificationRequest struct {
	Method    VerificationMethod `json:"method" binding:"required"`
	Reference string             `json:"reference" binding:"required"`
}

func (h *Handler) CreateVerification(c *gin.Context) {
	var req createVerificationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	userID := c.GetString("userID")
	artifactID := c.Param("artifactID")

	verification := &Verification{
		ArtifactID: artifactID,
		Method:     req.Method,
		Reference:  req.Reference,
	}

	if err := h.service.CreateVerification(c.Request.Context(), userID, verification); err != nil {
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

	c.JSON(http.StatusCreated, gin.H{
		"verification": verification,
	})
}

func (h *Handler) ListVerificationsByArtifact(c *gin.Context) {
	userID := c.GetString("userID")
	artifactID := c.Param("artifactID")

	verifications, err := h.service.ListVerificationsByArtifact(c.Request.Context(), userID, artifactID)
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
		"verifications": verifications,
	})
}

func (h *Handler) GetVerificationByID(c *gin.Context) {
	userID := c.GetString("userID")
	verificationID := c.Param("verificationID")

	verification, err := h.service.GetVerificationByID(c.Request.Context(), userID, verificationID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrVerificationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"verification": verification,
	})
}

func RegisterVerificationRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/deals/:dealID/artifacts/:artifactID/verifications")

	group.POST("", h.CreateVerification)
	group.GET("", h.ListVerificationsByArtifact)
	group.GET("/:verificationID", h.GetVerificationByID)
}
