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

func (h *Handler) DisputeDeal(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")

	deal, err := h.service.DisputeDeal(c.Request.Context(), email, dealID)
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
		"message": "dispute raised",
		"deal":    deal,
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
		"deal":    deal,
	})
}
