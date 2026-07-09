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

type createDealRequest struct {
	Title          string `json:"title" binding:"required"`
	AmountSats     int64  `json:"amount_sats" binding:"required"`
	SourcePlatform string `json:"source_platform" binding:"required"`
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
		Title:          req.Title,
		AmountSats:     req.AmountSats,
		SourcePlatform: req.SourcePlatform,
	}

	if err := h.service.CreateDeal(c.Request.Context(), deal); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"deal": deal,
	})
}

func (h *Handler) GetDealByID(c *gin.Context) {
	userID := c.GetString("userID")
	dealID := c.Param("dealID")

	deal, err := h.service.GetDealByID(c.Request.Context(), dealID, userID)
	if err != nil {
		switch {
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

	deals, err := h.service.ListByFreelancer(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
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
}
