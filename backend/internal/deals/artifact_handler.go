package deals

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// maxKindFieldBytes caps the tiny "kind" form field, which is read fully
	// into memory (the file part is streamed instead).
	maxKindFieldBytes = 64
	// multipartEnvelopeSlack is the headroom allowed above the artifact byte
	// cap for MIME boundaries, part headers, and small form fields when the
	// whole request body is bounded.
	multipartEnvelopeSlack = 1 << 20 // 1 MiB
)

// CreateArtifact accepts a multipart upload: the artifact kind in a "kind"
// form field and the file itself in an "artifact" field. The file is streamed
// to storage; the DB row records the resulting storage key.
func (h *Handler) CreateArtifact(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	// Stream the multipart body instead of gin's FormFile (which buffers the
	// whole request in memory via ParseMultipartForm before the size cap runs).
	// The per-artifact cap is still enforced in UploadArtifact; here we only
	// bound total request size so a huge envelope can't even be walked.
	if cap := h.service.MaxUploadBytes(); cap > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cap+multipartEnvelopeSlack)
	}

	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart request"})
		return
	}

	var (
		kind    ArtifactKind
		hasKind bool
	)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload exceeds the configured limit"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "malformed multipart request"})
			return
		}

		switch part.FormName() {
		case "kind":
			b, err := io.ReadAll(io.LimitReader(part, maxKindFieldBytes))
			part.Close()
			if err != nil || len(strings.TrimSpace(string(b))) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid artifact kind"})
				return
			}
			kind = ArtifactKind(strings.TrimSpace(string(b)))
			hasKind = true
		case "artifact":
			filename := part.FileName()
			if filename == "" {
				part.Close()
				c.JSON(http.StatusBadRequest, gin.H{"error": "artifact file is required (multipart field \"artifact\")"})
				return
			}
			if !hasKind {
				part.Close()
				c.JSON(http.StatusBadRequest, gin.H{"error": "the \"kind\" form field must appear before the \"artifact\" file"})
				return
			}

			artifact, err := h.service.UploadArtifact(c.Request.Context(), userID, dealID, kind, filename, part)
			part.Close()
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
			return
		default:
			part.Close()
		}
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "artifact file is required (multipart field \"artifact\")"})
}

func (h *Handler) ListArtifactsByDeal(c *gin.Context) {
	userID := c.GetString("userID")
	email := c.GetString("email")
	dealID := c.Param("dealID")

	artifacts, err := h.service.ListArtifactsByDeal(c.Request.Context(), userID, email, dealID)
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
	email := c.GetString("email")
	artifactID := c.Param("artifactID")
	dealID := c.Param("dealID")

	artifact, err := h.service.GetArtifactByID(c.Request.Context(), userID, email, dealID, artifactID)
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
