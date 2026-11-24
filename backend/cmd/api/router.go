package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"git.intruders.space/public/opentimestamps/verifyer"
	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/cv"
	"github.com/Amonochuka/ganji-backend/internal/deals"
	"github.com/Amonochuka/ganji-backend/internal/email"
	"github.com/Amonochuka/ganji-backend/internal/health"
	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/lnd"
	"github.com/Amonochuka/ganji-backend/internal/middleware"
	"github.com/Amonochuka/ganji-backend/internal/ots"
	"github.com/Amonochuka/ganji-backend/internal/storage"
	"github.com/Amonochuka/ganji-backend/internal/webhook"
)

func redactShareTokenPath(path string) string {
	for _, prefix := range []string{"/api/public/deals/", "/public/deals/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if idx := strings.Index(rest, "/"); idx >= 0 {
			return prefix + ":shareToken" + rest[idx:]
		}
		return prefix + ":shareToken"
	}
	return path
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		c.Next()
	}
}

// setupRouter builds the Gin engine and registers all routes. As we add
// deals, lightning, and cv, each one registers its own routes here via
// its own RegisterRoutes-style function — this file should never grow
// route logic directly, only wiring. The *deals.Service is returned so main
// can run background workers (e.g. the hold-expiry sweep) against it.
// uploadStore is the artifact blob backend, owned (and closed) by main.
func setupRouter(cfg *config.Config, dbConn *sql.DB, uploadStore storage.Storage) (*gin.Engine, *deals.Service, *cv.Service, *auth.Service) {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s | %s | %s %s | %d | %s | %s\n",
			param.TimeStamp.Format(time.RFC3339),
			param.ClientIP,
			param.Method,
			redactShareTokenPath(param.Path),
			param.StatusCode,
			param.Latency,
			param.ErrorMessage,
		)
	}))
	router.Use(securityHeaders())
	router.Use(middleware.NewCORSMiddleware(cfg.FrontendURLs, cfg.CORSAllowPrivateLAN))

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

	var acceptedHoldChecker deals.AcceptedHoldChecker
	if cfg.LNDRESTURL != "" {
		client, err := lnd.NewClient(lnd.Config{
			URL:          cfg.LNDRESTURL,
			CertPath:     cfg.LNDRESTCertPath,
			MacaroonPath: cfg.LNDRESTMacaroonPath,
		})
		if err != nil {
			log.Fatalf("configure LND accepted-hold checker: %v", err)
		}
		acceptedHoldChecker = client
	}

	emailSvc := email.NewService(cfg, authRepo)
	dealOptions := []deals.Option{
		deals.WithCVAnchorer(cvService),
		deals.WithStorage(uploadStore, cfg.MaxUploadBytes),
		deals.WithNotifier(emailSvc),
	}
	if acceptedHoldChecker != nil {
		dealOptions = append(dealOptions, deals.WithAcceptedHoldChecker(acceptedHoldChecker))
	}
	dealService := deals.NewService(dealRepo, lnbitsClient, dealOptions...)
	dealHandler := deals.NewHandler(dealService)

	router.GET("/health", health.Handler(dbConn, lnbitsClient))

	protected := router.Group("/")
	protected.Use(middleware.AuthRequired(tokenManager, authRepo))

	auth.RegisterProtectedRoutes(protected, authHandler)

	deals.RegisterRoutes(protected, dealHandler)
	publicDeals := router.Group("/public")
	publicDeals.Use(middleware.RateLimitByIP(60, time.Minute))
	publicDeals.GET("/deals/:shareToken", dealHandler.GetPublicDeal)
	publicDeals.GET("/deals/:shareToken/milestones", dealHandler.PublicListMilestones)
	publicDeals.POST("/deals/:shareToken/fund", dealHandler.FundDeal)
	publicClientActions := publicDeals.Group("")
	publicClientActions.Use(middleware.AuthRequired(tokenManager, authRepo))
	publicClientActions.POST("/deals/:shareToken/approve", dealHandler.PublicApproveDeal)
	publicClientActions.POST("/deals/:shareToken/payout/reset", dealHandler.PublicResetPayout)
	publicClientActions.POST("/deals/:shareToken/milestones/:milestoneID/accept-next", dealHandler.PublicAcceptNextMilestone)
	publicClientActions.POST("/deals/:shareToken/milestones/:milestoneID/reject", dealHandler.PublicRejectMilestone)
	publicClientActions.POST("/deals/:shareToken/exit", dealHandler.PublicExitDeal)
	deals.RegisterPublicArtifactRoutes(publicDeals, dealHandler)
	deals.RegisterArtifactRoutes(protected, dealHandler)
	deals.RegisterVerificationRoutes(protected, dealHandler)

	// Arbitration: dispute queue + resolution, operator-only.
	operator := protected.Group("/")
	operator.Use(middleware.OperatorRequired())
	deals.RegisterArbitrationRoutes(operator, dealHandler)

	webhookService := webhook.NewService(webhook.DealReader(dealRepo), lnbitsClient, emailSvc)
	if acceptedHoldChecker != nil {
		webhookService.SetAcceptedHoldChecker(acceptedHoldChecker)
	}
	webhookHandler := webhook.NewHandler(webhookService, cfg.LNBitsWebhookSecret, cfg.AllowInsecureWebhooks)
	webhookGroup := router.Group("/webhooks")
	webhookGroup.Use(middleware.RateLimitByIP(100, time.Minute))
	webhookGroup.POST("/lnbits", webhookHandler.HandleLNbitsWebhook)

	cv.RegisterRoutes(router, cv.NewHandler(cvService))

	return router, dealService, cvService, authService
}
