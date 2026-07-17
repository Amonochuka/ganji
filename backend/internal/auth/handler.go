package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler wires HTTP requests to the Service and TokenManager.
// It is responsible for request binding and HTTP responses.
// Business validation, hashing, and persistence belong to the Service.
type Handler struct {
	service *Service
	tokens  *TokenManager
}

func NewHandler(service *Service, tokens *TokenManager) *Handler {
	return &Handler{service: service, tokens: tokens}
}

type registerRequest struct {
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Signup handles POST /auth/signup
func (h *Handler) Signup(c *gin.Context) {
	var req registerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authResponse, err := h.service.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrEmailTaken),
			errors.Is(err, ErrSlugTaken):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusCreated, authResponse)
}

// Login handles POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authResponse, err := h.service.Authenticate(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, authResponse)
}

// RegisterRoutes mounts the auth routes onto the given router group.
func RegisterRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/auth")
	group.POST("/signup", h.Signup)
	group.POST("/login", h.Login)
}
