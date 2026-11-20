package deals

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type createMilestoneRequest struct {
	Title      string `json:"title" binding:"required"`
	AmountSats int64  `json:"amount_sats" binding:"required"`
	Order      int    `json:"order" binding:"required,min=1"`
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
			ID:         milestone.ID,
			Title:      milestone.Title,
			AmountSats: milestone.AmountSats,
			Order:      milestone.Order,
			Status:     string(milestone.Status),
			CreatedAt:  milestone.CreatedAt,
		},
	})
}

type milestoneStatusUpdateRequest struct {
	Status      string `json:"status" binding:"required"`
	CompletedAt string `json:"completed_at,omitempty"` // ISO 8601 or empty
}

type updateMilestoneResponse struct {
	Message   string        `json:"message"`
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
			ID:          milestones[i].ID,
			Title:       milestones[i].Title,
			AmountSats:  milestones[i].AmountSats,
			Order:       milestones[i].Order,
			Status:      string(milestones[i].Status),
			CompletedAt: &completedAtStr,
			CreatedAt:   milestones[i].CreatedAt,
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
