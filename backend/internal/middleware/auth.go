package middleware

import (
	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func AuthRequired(tokens *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}
		const prefix = "Bearer "

		if !strings.HasPrefix(header, prefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			c.Abort()
			return
		}
		token := strings.TrimPrefix(header, prefix)
		claims, err := tokens.VerifyAccessToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			c.Abort()
			return
		}

		c.Set("userID", claims.UserID)
		c.Next()
	}
}
