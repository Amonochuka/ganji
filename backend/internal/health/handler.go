package health

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/gin-gonic/gin"
)

// Handler returns the /health route handler. Confirms the server is up
// AND can reach the database AND LNbits. Hit this first whenever something feels broken.
func Handler(dbConn *sql.DB, lnbitsClient *lnbits.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		status := gin.H{"status": "ok"}

		if err := dbConn.PingContext(ctx); err != nil {
			status["status"] = "error"
			status["db"] = "unreachable"
		} else {
			status["db"] = "connected"
		}

		if lnbitsClient != nil {
			if err := lnbitsClient.HealthCheck(ctx); err != nil {
				status["status"] = "error"
				status["lnbits"] = "unreachable"
			} else {
				status["lnbits"] = "connected"
			}
		}

		httpStatus := http.StatusOK
		if status["status"] == "error" {
			httpStatus = http.StatusServiceUnavailable
		}

		c.JSON(httpStatus, status)
	}
}
