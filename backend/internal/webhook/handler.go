package webhook

import (
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// HandleLNbitsWebhook receives payment notifications from LNbits. This is a
// public endpoint — no JWT auth. The HMAC signature is verified in the
// service layer using LNBITS_WEBHOOK_SECRET.
func (h *Handler) HandleLNbitsWebhook(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var notification PaymentNotification
	if err := c.ShouldBindJSON(&notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
		return
	}

	signatureHeader := c.GetHeader("LNbits-Signature")

	if err := h.service.HandlePayment(
		c.Request.Context(),
		rawBody,
		signatureHeader,
		&notification,
	); err != nil {
		log.Printf("webhook processing error: %v", err)
		c.JSON(http.StatusOK, gin.H{"status": "error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func RegisterRoutes(router gin.IRouter, h *Handler) {
	router.POST("/webhooks/lnbits", h.HandleLNbitsWebhook)
}
