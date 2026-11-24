package deals

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) authorizePublicClient(c *gin.Context) bool {
	if err := h.service.AuthorizePublicClient(c.Request.Context(), c.Param("shareToken"), c.GetString("email")); err != nil {
		respondPublicDealError(c, err, "public client authorization")
		return false
	}
	return true
}

func respondPublicDealError(c *gin.Context, err error, operation string) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
		c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
	case errors.Is(err, ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": safeDealMessage(err)})
	case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
	case errors.Is(err, ErrPayoutFailed):
		c.JSON(http.StatusBadGateway, gin.H{"error": safeDealMessage(err)})
	case errors.Is(err, ErrPayoutInFlight):
		c.JSON(http.StatusConflict, gin.H{"error": safeDealMessage(err)})
	default:
		log.Printf("%s failed: %v", operation, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

type fundDealRequest struct {
	WalletURL   string `json:"wallet_url" binding:"required"`
	WalletKey   string `json:"wallet_key" binding:"required"`
	MilestoneID string `json:"milestone_id,omitempty"`
}

func (h *Handler) FundDeal(c *gin.Context) {
	var req fundDealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet URL and key are required"})
		return
	}

	deal, err := h.service.FundDeal(c.Request.Context(), c.Param("shareToken"), req.MilestoneID, req.WalletURL, req.WalletKey)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition),
			errors.Is(err, ErrNoCheckingID):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrVerificationUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "escrow verification is not configured"})
		case errors.Is(err, ErrWalletPaymentFailed):
			c.JSON(http.StatusBadGateway, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			log.Printf("public funding failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "funding failed; check the deal status before retrying"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "escrow funded and held",
		"deal":    deal,
	})
}

func (h *Handler) GetPublicDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")

	deal, err := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
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

func (h *Handler) PublicApproveDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")
	if !h.authorizePublicClient(c) {
		return
	}

	_, err := h.service.PublicApproveDeal(c.Request.Context(), shareToken)
	if err != nil {
		respondPublicDealError(c, err, "public approve")
		return
	}

	pd, _ := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	c.JSON(http.StatusOK, gin.H{
		"message": "deal approved / milestone released (sequential holds)",
		"deal":    pd,
	})
}

func (h *Handler) PublicListMilestones(c *gin.Context) {
	shareToken := c.Param("shareToken")
	deal, err := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"milestones": deal.Milestones})
}

type publicResetPayoutRequest struct {
	MilestoneID string `json:"milestone_id"`
}

func (h *Handler) PublicResetPayout(c *gin.Context) {
	shareToken := c.Param("shareToken")
	if !h.authorizePublicClient(c) {
		return
	}

	var req publicResetPayoutRequest
	_ = c.ShouldBindJSON(&req)
	deal, err := h.service.PublicResetPayout(c.Request.Context(), shareToken, req.MilestoneID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusConflict, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			log.Printf("public payout reset failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "payout attempt cleared", "deal": deal})
}

type publicRejectRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) PublicAcceptNextMilestone(c *gin.Context) {
	shareToken := c.Param("shareToken")
	milestoneID := c.Param("milestoneID")
	if !h.authorizePublicClient(c) {
		return
	}
	deal, err := h.service.PublicAcceptNextMilestone(c.Request.Context(), shareToken, milestoneID)
	if err != nil {
		respondPublicDealError(c, err, "public accept-next")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "milestone accepted", "deal": deal})
}

func (h *Handler) PublicRejectMilestone(c *gin.Context) {
	shareToken := c.Param("shareToken")
	milestoneID := c.Param("milestoneID")
	if !h.authorizePublicClient(c) {
		return
	}
	var req publicRejectRequest
	_ = c.ShouldBindJSON(&req)
	deal, err := h.service.PublicRejectMilestone(c.Request.Context(), shareToken, milestoneID, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "milestone rejected", "deal": deal})
}

func (h *Handler) PublicExitDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")
	if !h.authorizePublicClient(c) {
		return
	}
	deal, err := h.service.PublicExitDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deal exited", "deal": deal})
}
