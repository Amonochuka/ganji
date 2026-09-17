package deals

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// CreateArtifact accepts a multipart upload: the artifact kind in a "kind"
// form field and the file itself in an "artifact" field. The file is streamed
// to storage; the DB row records the resulting storage key.
func (h *Handler) CreateArtifact(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	kind := ArtifactKind(c.PostForm("kind"))
	file, header, err := c.Request.FormFile("artifact")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "artifact file is required (multipart field \"artifact\")"})
		return
	}
	defer file.Close()

	artifact, err := h.service.UploadArtifact(c.Request.Context(), userID, dealID, kind, header.Filename, file)
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
	dealID := c.Param("dealID")

	artifact, err := h.service.GetArtifactByID(c.Request.Context(), userID, dealID, artifactID)
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

// DownloadArtifact streams a deal artifact's stored content to the caller.
// Both parties to the deal may download: the freelancer and the client who is
// reviewing (or already accepted) the work.
func (h *Handler) DownloadArtifact(c *gin.Context) {
	userID := c.GetString("userID")
	email := c.GetString("email")
	dealID := c.Param("dealID")
	artifactID := c.Param("artifactID")

	artifact, reader, size, err := h.service.DownloadArtifact(c.Request.Context(), userID, email, dealID, artifactID)
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
	defer reader.Close()

	c.DataFromReader(
		http.StatusOK,
		size,
		contentTypeFor(artifact.StorageKey),
		reader,
		map[string]string{
			"Content-Disposition": `attachment; filename="` + filepath.Base(artifact.StorageKey) + `"`,
		},
	)
}

// contentTypeFor guesses a download Content-Type from the storage key's
// extension (which is sanitized at upload time), falling back to
// application/octet-stream.
func contentTypeFor(key string) string {
	if ct := mime.TypeByExtension(filepath.Ext(key)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func RegisterArtifactRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/deals/:dealID/artifacts")

	group.POST("", h.CreateArtifact)
	group.GET("", h.ListArtifactsByDeal)
	group.GET("/:artifactID", h.GetArtifactByID)
	group.GET("/:artifactID/download", h.DownloadArtifact)
}
