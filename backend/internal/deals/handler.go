package deals

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type updateDealStatusRequest struct {
	Status Status `json:"status" binding:"required"`
}

type createDealRequest struct {
	Title          string `json:"title" binding:"required"`
	AmountSats     int64  `json:"amount_sats" binding:"required"`
	SourcePlatform string `json:"source_platform" binding:"required"`
	ClientEmail    string `json:"client_email" binding:"required"`
	PayeeInvoice   string `json:"payee_invoice" binding:"required"`
}

func (h *Handler) CreateDeal(c *gin.Context) {
	var req createDealRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	userID := c.GetString("userID")

	deal := &Deal{
		FreelancerID:   userID,
		ClientEmail:    req.ClientEmail,
		Title:          req.Title,
		AmountSats:     req.AmountSats,
		SourcePlatform: req.SourcePlatform,
		PayeeInvoice:   req.PayeeInvoice,
	}

	if err := h.service.CreateDeal(c.Request.Context(), deal); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"deal": deal,
	})
}

func (h *Handler) GetDealByID(c *gin.Context) {
	userID := c.GetString("userID")
	email := c.GetString("email")
	dealID := c.Param("dealID")

	deal, err := h.service.GetDealByID(c.Request.Context(), dealID, userID, email)
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
		"deal": deal,
	})
}

func (h *Handler) ListDeals(c *gin.Context) {
	userID := c.GetString("userID")
	email := c.GetString("email")

	deals, err := h.service.ListByUser(c.Request.Context(), userID, email)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deals": deals,
	})
}

func RegisterRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/deals")

	group.POST("", h.CreateDeal)
	group.GET("", h.ListDeals)
	group.GET("/:dealID", h.GetDealByID)
	group.GET("/:dealID/payment", h.CheckPayment)
	group.PATCH("/:dealID/status", h.UpdateDealStatus)
	group.POST("/:dealID/submit", h.SubmitWork)
	group.POST("/:dealID/approve", h.ApproveDeal)
	group.POST("/:dealID/dispute", h.DisputeDeal)
	group.PATCH("/:dealID/payee-invoice", h.UpdatePayeeInvoice)
	group.POST("/:dealID/share-link", h.RotateShareLink)
}

// RegisterPublicRoutes registers the public (unauthenticated) deal routes.
// These are mounted on the root router, not the protected group.
func RegisterPublicRoutes(router gin.IRouter, h *Handler) {
	router.GET("/public/deals/:shareToken", h.GetPublicDeal)
}

func (h *Handler) UpdateDealStatus(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	var req updateDealStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	if err := h.service.UpdateStatus(c.Request.Context(), dealID, userID, req.Status); err != nil {
		switch {
		case errors.Is(err, ErrInvalidTransition),
			errors.Is(err, ErrInvalidInput):
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
		"message": "deal status updated",
	})
}

func (h *Handler) CheckPayment(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	deal, err := h.service.CheckPayment(c.Request.Context(), userID, dealID)
	if err != nil {
		switch {
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, ErrNoCheckingID):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deal": deal,
	})
}

func (h *Handler) SubmitWork(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	deal, err := h.service.SubmitWork(c.Request.Context(), userID, dealID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
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
		"message": "work submitted",
		"deal":    deal,
	})
}

func (h *Handler) ApproveDeal(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")

	deal, err := h.service.ApproveDeal(c.Request.Context(), email, dealID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
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
		"message": "deal approved and escrow released",
		"deal":    deal,
	})
}

type disputeRequest struct {
	Reason string `json:"reason" binding:"required"`
}

func (h *Handler) DisputeDeal(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")

	var req disputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "dispute reason is required",
		})
		return
	}

	deal, err := h.service.DisputeDeal(c.Request.Context(), email, dealID, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
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
		"message": "dispute raised — funds frozen pending arbitration",
		"deal":    deal,
	})
}

type resolveDisputeRequest struct {
	Resolution DisputeResolution `json:"resolution" binding:"required"`
}

// ListDisputes returns the arbitration queue. Mounted behind
// middleware.OperatorRequired, so by the time we get here the caller is an
// operator.
func (h *Handler) ListDisputes(c *gin.Context) {
	disputed, err := h.service.ListDisputes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"disputes": disputed})
}

// ResolveDispute closes a frozen dispute as an operator: release (settle +
// payout the freelancer) or refund (cancel the hold back to the client).
func (h *Handler) ResolveDispute(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")

	var req resolveDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resolution is required"})
		return
	}

	deal, err := h.service.ResolveDispute(c.Request.Context(), email, dealID, req.Resolution)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	message := "dispute resolved — escrow released to the freelancer"
	if deal.Status == StatusRefunded {
		message = "dispute resolved — escrow refunded to the client"
	}

	c.JSON(http.StatusOK, gin.H{
		"message": message,
		"deal":    deal,
	})
}

// RegisterArbitrationRoutes mounts the operator-only arbitration endpoints.
// Callers must already attach middleware.OperatorRequired to the router.
func RegisterArbitrationRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/disputes")
	group.GET("", h.ListDisputes)
	group.POST("/:dealID/resolve", h.ResolveDispute)
}

type payeeInvoiceRequest struct {
	PayeeInvoice string `json:"payee_invoice" binding:"required"`
}

func (h *Handler) UpdatePayeeInvoice(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	var req payeeInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	deal, err := h.service.UpdatePayeeInvoice(c.Request.Context(), userID, dealID, req.PayeeInvoice)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
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
		"message": "payee invoice updated",
		"deal":    deal,
	})
}

func (h *Handler) GetPublicDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")

	deal, err := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		"deal": deal,
	})
}

func (h *Handler) RotateShareLink(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	deal, err := h.service.RotateShareLink(c.Request.Context(), userID, dealID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
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
		"message": "share link regenerated",
		"deal":    deal,
	})
}
