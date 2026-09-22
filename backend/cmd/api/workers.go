package main

import (
	"context"
	"log"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/cv"
	"github.com/Amonochuka/ganji-backend/internal/deals"
)

// runWorkers starts the background jobs that reconcile escrow state with
// LNbits and upgrade OpenTimestamps proofs. It blocks until ctx is cancelled;
// call it as a goroutine.
func runWorkers(ctx context.Context, cfg *config.Config, dealService *deals.Service, cvService *cv.Service) {
	lifetime := time.Duration(cfg.HoldInvoiceExpirySeconds) * time.Second
	interval := time.Duration(cfg.HoldSweepIntervalSeconds) * time.Second

	deals.RunHoldSweep(ctx, dealService, interval, lifetime)

	otsInterval := time.Duration(cfg.OTSUpgradeIntervalSeconds) * time.Second
	runOTSUpgradeLoop(ctx, cvService, otsInterval)
}

// runOTSUpgradeLoop periodically asks the OpenTimestamps calendars whether
// pending proofs have been mined into a Bitcoin block, and stores the upgraded
// (bitcoin-attested) proof when they have. The first check fires immediately,
// then every interval.
func runOTSUpgradeLoop(ctx context.Context, cvService *cv.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if err := cvService.UpgradeOTSProofs(runCtx); err != nil {
			log.Printf("cv: ots upgrade worker error: %v", err)
		}
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
