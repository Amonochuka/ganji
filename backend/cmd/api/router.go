package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"git.intruders.space/public/opentimestamps/verifyer"
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
func setupRouter(cfg *config.Config, dbConn *sql.DB, uploadStore storage.Storage) (*gin.Engine, *deals.Service, *cv.Service, *auth.Service) {
	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.FrontendURLs,
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTRefreshSecret)

	authRepo := auth.NewRepository(dbConn)
	authService := auth.NewService(authRepo, tokenManager)

	// Promote the configured operator emails (OPERATOR_EMAILS) so their next
	// login mints access tokens with is_operator=true. Safe on every boot.
	if err := authService.ApplyOperatorRole(context.Background(), cfg.OperatorEmails); err != nil {
		log.Fatalf("promoting operators: %v", err)
	}

	// Apply rate limiting to auth routes
	authGroup := router.Group("/auth")
	authGroup.Use(middleware.RateLimitAuth())
	authHandler := auth.NewHandler(authService)
	authGroup.POST("/signup", authHandler.Signup)
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.POST("/refresh-token", authHandler.RefreshToken)

	dealRepo := deals.NewRepository(dbConn, cfg.PreimageEncryptionKey)

	// OpenTimestamps client for blockchain anchoring
	otsClient := ots.NewClient()

	// OTS proof verifier. Offline proof verification is always available;
	// when OTS_ESPLORA_URL is set, attested block heights are additionally
	// checked against the live Bitcoin chain (full block-header verification).
	otsVerifier := ots.NewVerifier()
	if cfg.OTSEsploraURL != "" {
		otsVerifier = ots.NewVerifierWithChain(
			verifyer.NewEsploraClient(cfg.OTSEsploraURL, time.Duration(cfg.OTSEsploraTimeoutSeconds)*time.Second),
		)
	}

	cvService := cv.NewService(cv.NewRepository(dbConn), uploadStore, otsClient, cv.WithVerifier(otsVerifier))

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

	auth.RegisterProtectedRoutes(protected, authHandler)

	deals.RegisterRoutes(protected, dealHandler)
	publicDeals := router.Group("/public")
	publicDeals.Use(middleware.RateLimitByIP(60, time.Minute))
	publicDeals.GET("/deals/:shareToken", dealHandler.GetPublicDeal)
	publicDeals.POST("/deals/:shareToken/approve", dealHandler.PublicApproveDeal)
	deals.RegisterArtifactRoutes(protected, dealHandler)
	deals.RegisterVerificationRoutes(protected, dealHandler)

	// Arbitration: dispute queue + resolution, operator-only.
	operator := protected.Group("/")
	operator.Use(middleware.OperatorRequired())
	deals.RegisterArbitrationRoutes(operator, dealHandler)

	webhookService := webhook.NewService(webhook.DealReader(dealRepo), lnbitsClient, emailSvc)
	webhookHandler := webhook.NewHandler(webhookService, cfg.LNBitsWebhookSecret)
	webhookGroup := router.Group("/webhooks")
	webhookGroup.Use(middleware.RateLimitByIP(100, time.Minute))
	webhookGroup.POST("/lnbits", webhookHandler.HandleLNbitsWebhook)

	cv.RegisterRoutes(router, cv.NewHandler(cvService))

	return router, dealService, cvService, authService
}
