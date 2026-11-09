package deals

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type MilestoneView struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	AmountSats  int64     `json:"amount_sats"`
	Order       int       `json:"order"`
	Status      string    `json:"status"`
	Invoice     string    `json:"invoice,omitempty"`
	CompletedAt *string   `json:"completed_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

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

type milestoneInput struct {
	Title      string `json:"title" binding:"required"`
	AmountSats int64  `json:"amount_sats" binding:"required"`
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
		ms = append(ms, MilestoneInput{Title: mi.Title, AmountSats: mi.AmountSats})
	}

	if err := h.service.CreateDealWithMilestones(c.Request.Context(), deal, ms); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			log.Printf("create deal failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "payment provider error: could not create escrow (deal or milestone). Your LNbits wallet must support hold (HODL) invoices — see backend/.env LNBITS_URL.",
			})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"deal": deal.View(),
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
		"deal": deal.View(),
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
		"deals": dealsView(deals),
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
	group.POST("/:dealID/milestones", h.CreateMilestone)
	group.GET("/:dealID/milestones", h.ListMilestones)
	group.PATCH("/:dealID/milestones/:milestoneID", h.UpdateMilestone)
	group.POST("/:dealID/exit", h.ExitDeal)
}

// RegisterPublicRoutes registers the public (unauthenticated) deal routes.
// These are mounted on the root router, not the protected group.
func RegisterPublicRoutes(router gin.IRouter, h *Handler) {
	public := router.Group("/public")
	public.GET("/deals/:shareToken", h.GetPublicDeal)
	public.GET("/deals/:shareToken/milestones", h.PublicListMilestones)
	public.POST("/deals/:shareToken/approve", h.PublicApproveDeal)
	public.POST("/deals/:shareToken/milestones/:milestoneID/accept-next", h.PublicAcceptNextMilestone)
	public.POST("/deals/:shareToken/milestones/:milestoneID/reject", h.PublicRejectMilestone)
	public.POST("/deals/:shareToken/exit", h.PublicExitDeal)
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
		"deal":    deal.View(),
	})
}

// RegisterArbitrationRoutes mounts the operator-only arbitration endpoints.
// Callers must already attach middleware.OperatorRequired to the router.
func RegisterArbitrationRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/disputes")
	group.GET("", h.ListDisputes)
	group.POST("/:dealID/resolve", h.ResolveDispute)

	// Operator reconcile for hung payouts: the human backstop to the
	// no-auto-resend payout policy (see Service.ReconcilePayout). Mounted here
	// so it inherits the same operator-only gate.
	reconcile := router.Group("/deals")
	reconcile.POST("/:dealID/reconcile", h.ReconcileDeal)
}

type reconcileDealRequest struct {
	Action           ReconcileAction `json:"action" binding:"required"`
	PayoutCheckingID string          `json:"payout_checking_id"`
}

// ReconcileDeal lets an operator unstick a deal whose payout is hung. They
// confirmed the true state against LNbits history and either record a verified
// payout (confirm_payout -> release) or clear a never-sent attempt
// (reset_payout -> re-payable by a normal approve/release).
func (h *Handler) ReconcileDeal(c *gin.Context) {
	email := c.GetString("email")
	dealID := c.Param("dealID")

	var req reconcileDealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	deal, err := h.service.ReconcilePayout(c.Request.Context(), email, dealID, req.Action, req.PayoutCheckingID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, ErrPayoutInFlight):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deal": deal.View(),
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

func (h *Handler) PublicApproveDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")

	deal, err := h.service.PublicApproveDeal(c.Request.Context(), shareToken)
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
			log.Printf("public approve failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	pd, _ := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	c.JSON(http.StatusOK, gin.H{
		"message": "deal approved / milestone released (sequential holds)",
		"deal":    pd,
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

type createMilestoneRequest struct {
	Title       string `json:"title" binding:"required"`
	AmountSats  int64  `json:"amount_sats" binding:"required"`
	Order       int    `json:"order" binding:"required,min=1"`
}

type listMilestonesResponse struct {
	Milestones []MilestoneView `json:"milestones"`
}

type milestoneView struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	AmountSats  int64     `json:"amount_sats"`
	Order       int       `json:"order"`
	Status      string    `json:"status"`
	CompletedAt *string   `json:"completed_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *Handler) CreateMilestone(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	var req createMilestoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	deal, err := h.service.GetDealByID(c.Request.Context(), dealID, userID, c.GetString("email"))
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

	if deal.FreelancerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the freelancer can create milestones"})
		return
	}

	milestone, err := h.service.CreateMilestone(c.Request.Context(), dealID, req.Title, req.AmountSats, req.Order)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "milestone created",
		"milestone": milestoneView{
			ID:        milestone.ID,
			Title:     milestone.Title,
			AmountSats: milestone.AmountSats,
			Order:     milestone.Order,
			Status:    string(milestone.Status),
			CreatedAt: milestone.CreatedAt,
		},
	})
}

type milestoneStatusUpdateRequest struct {
	Status      string `json:"status" binding:"required"`
	CompletedAt string `json:"completed_at,omitempty"` // ISO 8601 or empty
}

type updateMilestoneResponse struct {
	Message string          `json:"message"`
	Milestone milestoneView `json:"milestone"`
}

func (h *Handler) ListMilestones(c *gin.Context) {
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

	// Only freelancer or client can list milestones
	isFreelancer := deal.FreelancerID == userID
	isClient := email != "" && strings.EqualFold(deal.ClientEmail, email)

	if !isFreelancer && !isClient {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	milestones, err := h.service.GetMilestonesByDeal(c.Request.Context(), dealID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	views := make([]MilestoneView, 0, len(milestones))
	for i := range milestones {
		completedAtStr := ""
		if milestones[i].CompletedAt.Valid {
			completedAtStr = milestones[i].CompletedAt.Time.Format(time.RFC3339)
		}
		views = append(views, MilestoneView{
			ID:         milestones[i].ID,
			Title:      milestones[i].Title,
			AmountSats: milestones[i].AmountSats,
			Order:      milestones[i].Order,
			Status:     string(milestones[i].Status),
			CompletedAt: &completedAtStr,
			CreatedAt:  milestones[i].CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"milestones": views,
	})
}

func (h *Handler) UpdateMilestone(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")
	milestoneID := c.Param("milestoneID")

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

	// Only freelancer can update milestones
	if deal.FreelancerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the freelancer can update milestones"})
		return
	}

	var req milestoneStatusUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var completedAt *time.Time
	if req.CompletedAt != "" {
		t, err := time.Parse(time.RFC3339, req.CompletedAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid completed_at format"})
			return
		}
		completedAt = &t
	}

	if err := h.service.UpdateMilestoneStatus(c.Request.Context(), milestoneID, MilestoneStatus(req.Status), completedAt); err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	// Get the updated milestone
	h.service.GetMilestoneByID(c.Request.Context(), milestoneID)

	// Notify about milestone move - we'll improve this later
	// For now, just return success.

	c.JSON(http.StatusOK, gin.H{
		"message": "milestone updated",
	})
}

type exitDealRequest struct{}

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

	// Only client can exit the deal
	isClient := email != "" && strings.EqualFold(deal.ClientEmail, email)
	if !isClient {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the client can exit the deal"})
		return
	}

	// Exit the deal - mark it as exited and notify freelancer
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
		"message": "deal exited by client",
	})
}

func (h *Handler) PublicListMilestones(c *gin.Context) {
	shareToken := c.Param("shareToken")
	deal, err := h.service.GetPublicDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"milestones": deal.Milestones})
}

type publicRejectRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) PublicAcceptNextMilestone(c *gin.Context) {
	shareToken := c.Param("shareToken")
	milestoneID := c.Param("milestoneID")
	deal, err := h.service.PublicAcceptNextMilestone(c.Request.Context(), shareToken, milestoneID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "milestone accepted", "deal": deal})
}

func (h *Handler) PublicRejectMilestone(c *gin.Context) {
	shareToken := c.Param("shareToken")
	milestoneID := c.Param("milestoneID")
	var req publicRejectRequest
	_ = c.ShouldBindJSON(&req)
	deal, err := h.service.PublicRejectMilestone(c.Request.Context(), shareToken, milestoneID, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound), errors.Is(err, ErrMilestoneNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "milestone rejected", "deal": deal})
}

func (h *Handler) PublicExitDeal(c *gin.Context) {
	shareToken := c.Param("shareToken")
	deal, err := h.service.PublicExitDeal(c.Request.Context(), shareToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTransition):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrDealNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deal exited", "deal": deal})
}

func milestoneViewFrom(m Milestone) MilestoneView {
	var completed *string
	if m.CompletedAt.Valid {
		s := m.CompletedAt.Time.Format(time.RFC3339)
		completed = &s
	}
	return MilestoneView{ID: m.ID, Title: m.Title, AmountSats: m.AmountSats, Order: m.Order, Status: string(m.Status), Invoice: m.Invoice, CompletedAt: completed, CreatedAt: m.CreatedAt}
}
