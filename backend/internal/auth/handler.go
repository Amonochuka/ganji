package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const refreshCookieName = "ganji_refresh"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
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

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func safeAuthMessage(err error) string {
	switch {
	case errors.Is(err, ErrInvalidEmail):
		return "invalid email format"
	case errors.Is(err, ErrPasswordTooShort):
		return "password must be at least 8 characters"
	case errors.Is(err, ErrEmailTaken):
		return "email is already registered"
	case errors.Is(err, ErrSlugTaken):
		return "could not generate a unique profile slug"
	case errors.Is(err, ErrInvalidCredentials):
		return "invalid email or password"
	case errors.Is(err, ErrInvalidToken):
		return "invalid or expired token"
	case errors.Is(err, ErrInvalidInput):
		return "invalid request"
	default:
		return "internal server error"
	}
}

func authResponseJSON(resp *AuthResponse) gin.H {
	return gin.H{
		"user":         resp.User,
		"access_token": resp.AccessToken,
	}
}

func cookieSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	proto := c.GetHeader("X-Forwarded-Proto")
	if idx := strings.Index(proto, ","); idx >= 0 {
		proto = proto[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, token, int(RefreshTokenTTL().Seconds()), "/auth", "", cookieSecure(c), true)
}

func clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(refreshCookieName, "", -1, "/auth", "", cookieSecure(c), true)
}

func requestRefreshToken(c *gin.Context) string {
	if cookie, err := c.Cookie(refreshCookieName); err == nil && cookie != "" {
		return cookie
	}
	var req refreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return ""
	}
	return strings.TrimSpace(req.RefreshToken)
}

func requestAccessToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimPrefix(header, prefix)
	}
	return ""
}

func (h *Handler) Signup(c *gin.Context) {
	var req registerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authResponse, err := h.service.Register(
		c.Request.Context(),
		req.Email,
		req.Password,
		req.DisplayName,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput),
			errors.Is(err, ErrEmailTaken),
			errors.Is(err, ErrSlugTaken):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeAuthMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	setRefreshCookie(c, authResponse.RefreshToken)
	c.JSON(http.StatusCreated, authResponseJSON(authResponse))
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	authResponse, err := h.service.Authenticate(
		c.Request.Context(),
		req.Email,
		req.Password,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			c.JSON(http.StatusBadRequest, gin.H{"error": safeAuthMessage(err)})
		case errors.Is(err, ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": safeAuthMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	setRefreshCookie(c, authResponse.RefreshToken)
	c.JSON(http.StatusOK, authResponseJSON(authResponse))
}

func (h *Handler) Logout(c *gin.Context) {
	refreshToken := requestRefreshToken(c)
	accessToken := requestAccessToken(c)
	clearRefreshCookie(c)

	if refreshToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	err := h.service.Logout(c.Request.Context(), refreshToken, accessToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidToken):
			c.JSON(http.StatusUnauthorized, gin.H{"error": safeAuthMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

func (h *Handler) RefreshToken(c *gin.Context) {
	refreshToken := requestRefreshToken(c)
	if refreshToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	authResponse, err := h.service.RefreshToken(c.Request.Context(), refreshToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidToken):
			c.JSON(http.StatusUnauthorized, gin.H{"error": safeAuthMessage(err)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	setRefreshCookie(c, authResponse.RefreshToken)
	c.JSON(http.StatusOK, authResponseJSON(authResponse))
}

func (h *Handler) GetMe(c *gin.Context) {
	userID := c.GetString("userID")

	user, err := h.service.GetMe(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

func RegisterRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/auth")
	group.POST("/signup", h.Signup)
	group.POST("/login", h.Login)
	group.POST("/logout", h.Logout)
	group.POST("/refresh-token", h.RefreshToken)
}

// RegisterProtectedRoutes mounts auth routes that require a valid access token.
// Callers must attach middleware.AuthRequired to the router first.
func RegisterProtectedRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/auth")
	group.GET("/me", h.GetMe)
}
