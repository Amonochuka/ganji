package main

import (
	"database/sql"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/health"
)

// setupRouter builds the Gin engine and registers all routes. As we add
// deals, lightning, and cv, each one registers its own routes here via
// its own RegisterRoutes-style function — this file should never grow
// route logic directly, only wiring.
func setupRouter(cfg *config.Config, dbConn *sql.DB) *gin.Engine {
	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.FrontendURL},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	router.GET("/health", health.Handler(dbConn))

	authRepo := auth.NewRepository(dbConn)
	authService := auth.NewService(authRepo)
	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTRefreshSecret)
	authHandler := auth.NewHandler(authService, tokenManager)
	auth.RegisterRoutes(router, authHandler)

	// Future: deals.RegisterRoutes(router, dbConn, cfg)
	// Future: lightning.RegisterRoutes(router, dbConn, cfg)
	// Future: cv.RegisterRoutes(router, dbConn, cfg)

	return router
}
