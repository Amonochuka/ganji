package deals

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

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

	c.JSON(http.StatusOK, gin.H{"disputes": dealsView(disputed)})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrPayoutFailed):
			c.JSON(http.StatusBadGateway, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusConflict, gin.H{"error": safeDealMessage(err)})
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
		"deal":    deal.View(),
	})
}

// RegisterArbitrationRoutes mounts the operator-only arbitration endpoints.
// Callers must already attach middleware.OperatorRequired to the router.

func (h *Handler) ResolveMilestone(c *gin.Context) {
	operatorEmail := c.GetString("email")
	dealID := c.Param("dealID")
	milestoneID := c.Param("milestoneID")
	var req resolveDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resolution is required"})
		return
	}
	m, err := h.service.ResolveMilestone(c.Request.Context(), operatorEmail, dealID, milestoneID, req.Resolution)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	msg := "milestone dispute resolved — escrow released to the freelancer"
	if req.Resolution == DisputeResolutionRefund {
		msg = "milestone dispute resolved — escrow refunded to the client"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "milestone_id": m.ID, "status": m.Status})
}

func RegisterArbitrationRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/disputes")
	group.GET("", h.ListDisputes)
	group.POST("/:dealID/resolve", h.ResolveDispute)
	group.POST("/deals/:dealID/milestones/:milestoneID/resolve", h.ResolveMilestone)

	// Operator reconcile for hung milestone payouts.
	reconcile := router.Group("/deals")
	reconcile.POST("/:dealID/milestones/:milestoneID/reconcile", h.ReconcileMilestone)
}

type reconcileDealRequest struct {
	Action           ReconcileAction `json:"action" binding:"required"`
	PayoutCheckingID string          `json:"payout_checking_id"`
}

// ReconcileMilestone lets an operator unstick a milestone whose payout is hung.
func (h *Handler) ReconcileMilestone(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")
	milestoneID := c.Param("milestoneID")

	var req reconcileDealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	m, err := h.service.ReconcileMilestonePayout(c.Request.Context(), email, dealID, milestoneID, req.Action, req.PayoutCheckingID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": safeDealMessage(err)})
		case errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusConflict, gin.H{"error": safeDealMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"milestone": m, "milestone_id": m.ID, "status": m.Status})
}
