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

		c.Set("claims", claims)
		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("isOperator", claims.IsOperator)
		c.Next()
	}
}

// OperatorRequired gates a route to arbitration operators only. It reads the
// is_operator claim that AuthRequired placed in the context; anyone else gets
// 403 Forbidden. Operators are promoted via OPERATOR_EMAILS, never self-serve.
func OperatorRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !c.GetBool("isOperator") {
			c.JSON(http.StatusForbidden, gin.H{"error": "operator privileges required"})
			c.Abort()
			return
		}
		c.Next()
	}
}
