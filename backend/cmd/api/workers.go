package main

import (
	"context"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/deals"
)

// runWorkers starts the background jobs that reconcile escrow state with
// LNbits. It blocks until ctx is cancelled; call it as a goroutine.
func runWorkers(ctx context.Context, cfg *config.Config, dealService *deals.Service) {
	lifetime := time.Duration(cfg.HoldInvoiceExpirySeconds) * time.Second
	interval := time.Duration(cfg.HoldSweepIntervalSeconds) * time.Second

	deals.RunHoldSweep(ctx, dealService, interval, lifetime)
}
