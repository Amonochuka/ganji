package deals

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type updateDealStatusRequest struct {
	Status Status `json:"status" binding:"required"`
}

type milestoneInput struct {
	Title        string `json:"title" binding:"required"`
	AmountSats   int64  `json:"amount_sats" binding:"required"`
	DeliveryDays int    `json:"delivery_days"`
}

type generateInvoiceRequest struct {
	WalletURL  string `json:"wallet_url" binding:"required"`
	WalletKey  string `json:"wallet_key" binding:"required"`
	AmountSats int64  `json:"amount_sats" binding:"required"`
	Memo       string `json:"memo,omitempty"`
}

type createDealRequest struct {
	Title          string           `json:"title" binding:"required"`
	AmountSats     int64            `json:"amount_sats"` // required for legacy single-hold; derived from milestones when provided
	SourcePlatform string           `json:"source_platform" binding:"required"`
	ClientEmail    string           `json:"client_email" binding:"required"`
	PayeeInvoice   string           `json:"payee_invoice" binding:"required"`
	Milestones     []milestoneInput `json:"milestones,omitempty"` // sequential per-milestone holds (M1 funded first)
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

	var ms []MilestoneInput
	for _, mi := range req.Milestones {
		ms = append(ms, MilestoneInput{Title: mi.Title, AmountSats: mi.AmountSats, DeliveryDays: mi.DeliveryDays})
	}

	if err := h.service.CreateDealWithMilestones(c.Request.Context(), deal, ms); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			log.Printf("create deal failed (user=%s): %v", userID, err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "Could not create the deal. Check backend logs for the underlying error before retrying."})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"deal": h.service.dealViewWithMilestones(c.Request.Context(), deal),
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
		"deal": h.service.dealViewWithMilestones(c.Request.Context(), deal),
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

	// Include per-milestone truth in the list view (N per-deal lookups;
	// acceptable at freelancer-dashboard scale).
	views := make([]DealView, 0, len(deals))
	for i := range deals {
		views = append(views, h.service.dealViewWithMilestones(c.Request.Context(), &deals[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"deals": views,
	})
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

	if err := h.service.UpdateStatus(c.Request.Context(), userID, dealID, req.Status); err != nil {
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
		"deal": deal.View(),
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
		"deal":    deal.View(),
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
		case errors.Is(err, ErrPayoutFailed):
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		case errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "deal approved and escrow released",
		"deal":    deal.View(),
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
		"deal":    deal.View(),
	})
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
		"deal":    deal.View(),
	})
}

func (h *Handler) GenerateInvoice(c *gin.Context) {
	var req generateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	invoice, err := h.service.GenerateInvoice(c.Request.Context(), req.WalletURL, req.WalletKey, req.AmountSats, req.Memo)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrWalletInvoiceFailed):
			log.Printf("generate invoice failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		default:
			log.Printf("generate invoice failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "could not generate invoice with linked wallet"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"invoice": gin.H{
			"bolt11":       invoice.PaymentRequest,
			"payment_hash": invoice.PaymentHash,
			"checking_id":  invoice.CheckingID,
		},
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
		"deal":    deal.View(),
	})
}

func (h *Handler) ExitDeal(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	// Check user has access to this deal
	email := c.GetString("email")
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	// Symmetric exit: either participant (client or freelancer) may exit, per redesign.
	isClient := email != "" && strings.EqualFold(deal.ClientEmail, email)
	isFreelancer := userID != "" && deal.FreelancerID == userID
	if !isClient && !isFreelancer {
		c.JSON(http.StatusForbidden, gin.H{"error": "only deal participants can exit the deal"})
		return
	}

	if err := h.service.ExitDeal(c.Request.Context(), dealID); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "deal exited",
	})
}
