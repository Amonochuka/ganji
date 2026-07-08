package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler wires HTTP requests to Service and TokenManager. No validation
// or hashing here — that lives in Service. This file should only ever
// grow request/response plumbing.
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

	user, err := h.service.Register(req.Email, req.Password, req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailTaken), errors.Is(err, ErrSlugTaken),
			errors.Is(err, ErrInvalidEmail), errors.Is(err, ErrPasswordTooShort):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		}
		return
	}

	accessToken, err := h.tokens.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}
	refreshToken, err := h.tokens.GenerateRefreshToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user":   user,
		"tokens": tokenResponse{AccessToken: accessToken, RefreshToken: refreshToken},
	})
}

// Login handles POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	user, err := h.service.Authenticate(req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	accessToken, err := h.tokens.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}
	refreshToken, err := h.tokens.GenerateRefreshToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":   user,
		"tokens": tokenResponse{AccessToken: accessToken, RefreshToken: refreshToken},
	})
}

// RegisterRoutes mounts the auth routes onto the given router group.
func RegisterRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/auth")
	group.POST("/signup", h.Signup)
	group.POST("/login", h.Login)
}
