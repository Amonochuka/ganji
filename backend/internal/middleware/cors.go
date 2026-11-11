package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"time"
)

// NewCORSMiddleware returns a Gin middleware that allows the dev origin.
func NewCORSMiddleware() gin.HandlerFunc {
	config := cors.Config{
		// Allow the exact origin you are testing from.
		AllowOrigins: []string{
			"http://localhost:3000",
			"http://192.168.89.197:3000",
		},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
		// Allow credentials so cookies / Authorization header work.
		AllowCredentials: true,
		// Optional: set a reasonable max-age.
		MaxAge: 12 * time.Hour,
	}
	return cors.New(config)
}