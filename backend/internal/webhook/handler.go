package webhook

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
	secret  string
}

func NewHandler(service *Service, secret string) *Handler {
	return &Handler{service: service, secret: secret}
}

// HandleLNbitsWebhook receives payment notifications from LNbits.
// This is a public endpoint and does not require JWT authentication. When
// LNBITS_WEBHOOK_SECRET (the wallet's webhook_secret) is set, inbound
// requests must carry a valid LNbits-Signature: t=<unix>,v1=<hmac_sha256>.
func (h *Handler) HandleLNbitsWebhook(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	if h.secret != "" && !VerifyLNbitsSignature(rawBody, c.GetHeader("LNbits-Signature"), h.secret) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	var notification PaymentNotification
	if err := json.Unmarshal(rawBody, &notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
		return
	}

	if err := h.service.HandlePayment(
		c.Request.Context(),
		&notification,
	); err != nil {
		switch {
		case errors.Is(err, ErrMalformedPayload):
			c.JSON(http.StatusBadRequest, gin.H{"error": "malformed payload"})
		case errors.Is(err, ErrPaymentFailed):
			c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "payment not successful"})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "no deal for checking_id"})
		default:
			log.Printf("webhook processing error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal failure"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func RegisterRoutes(router gin.IRouter, h *Handler) {
	router.POST("/webhooks/lnbits", h.HandleLNbitsWebhook)
}
