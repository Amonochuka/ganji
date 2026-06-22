package health

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler returns the /health route handler. Confirms the server is up
// AND can reach the database. Hit this first whenever something feels broken.
func Handler(dbConn *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := dbConn.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"db":     "unreachable",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"db":     "connected",
		})
	}
}