package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/cv"
	"github.com/Amonochuka/ganji-backend/internal/deals"
	"github.com/Amonochuka/ganji-backend/internal/email"
	"github.com/Amonochuka/ganji-backend/internal/health"
	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/middleware"
	"github.com/Amonochuka/ganji-backend/internal/ots"
	"github.com/Amonochuka/ganji-backend/internal/storage"
	"github.com/Amonochuka/ganji-backend/internal/webhook"
)

// setupRouter builds the Gin engine and registers all routes. As we add
// deals, lightning, and cv, each one registers its own routes here via
// its own RegisterRoutes-style function — this file should never grow
// route logic directly, only wiring. The *deals.Service is returned so main
// can run background workers (e.g. the hold-expiry sweep) against it.
// uploadStore is the artifact blob backend, owned (and closed) by main.
func setupRouter(cfg *config.Config, dbConn *sql.DB, uploadStore storage.Storage) (*gin.Engine, *deals.Service, *cv.Service) {
	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.FrontendURL},
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTRefreshSecret)

	authRepo := auth.NewRepository(dbConn)
	authService := auth.NewService(authRepo, tokenManager)
	authHandler := auth.NewHandler(authService)
	auth.RegisterRoutes(router, authHandler)

	// Promote the configured operator emails (OPERATOR_EMAILS) so their next
	// login mints access tokens with is_operator=true. Safe on every boot.
	if err := authService.ApplyOperatorRole(context.Background(), cfg.OperatorEmails); err != nil {
		log.Fatalf("promoting operators: %v", err)
	}

	dealRepo := deals.NewRepository(dbConn)

	// OpenTimestamps client for blockchain anchoring
	otsClient := ots.NewClient()

	cvService := cv.NewService(cv.NewRepository(dbConn), uploadStore, otsClient)

	lnbitsClient := lnbits.NewClient(
		lnbits.Config{
			URL:           cfg.LNBitsURL,
			APIKey:        cfg.LNBitsAPIKey,
			AdminKey:      cfg.LNBitsAdminKey,
			WebhookURL:    cfg.WebhookURL,
			HoldExpirySec: cfg.HoldInvoiceExpirySeconds,
		},
	)

	emailSvc := email.NewService(cfg, authRepo)
	dealService := deals.NewService(dealRepo, lnbitsClient,
		deals.WithCVAnchorer(cvService),
		deals.WithStorage(uploadStore, cfg.MaxUploadBytes),
		deals.WithNotifier(emailSvc),
	)
	dealHandler := deals.NewHandler(dealService)

	router.GET("/health", health.Handler(dbConn, lnbitsClient))

	protected := router.Group("/")
	protected.Use(middleware.AuthRequired(tokenManager))

	deals.RegisterRoutes(protected, dealHandler)
	deals.RegisterPublicRoutes(router, dealHandler)
	deals.RegisterArtifactRoutes(protected, dealHandler)
	deals.RegisterVerificationRoutes(protected, dealHandler)

	// Arbitration: dispute queue + resolution, operator-only.
	operator := protected.Group("/")
	operator.Use(middleware.OperatorRequired())
	deals.RegisterArbitrationRoutes(operator, dealHandler)

	webhookService := webhook.NewService(webhook.DealReader(dealRepo), lnbitsClient, emailSvc)
	webhookHandler := webhook.NewHandler(webhookService, cfg.LNBitsWebhookSecret)
	webhook.RegisterRoutes(router, webhookHandler)

	cv.RegisterRoutes(router, cv.NewHandler(cvService))

	// Start OTS proof upgrade worker (runs every 6 hours)
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := cvService.UpgradeOTSProofs(ctx); err != nil {
				log.Printf("cv: ots upgrade worker error: %v", err)
			}
			cancel()
		}
	}()

	return router, dealService, cvService
}
