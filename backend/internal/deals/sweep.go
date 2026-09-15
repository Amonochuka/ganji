package deals

import (
	"context"
	"log"
	"time"
)

// RunHoldSweep periodically reconciles stale open deals against LNbits until
// the context is cancelled. It is meant to run as a background goroutine:
// the first sweep fires immediately, then every interval.
//
// Sweeping is deliberately conservative:
//   - only deals older than the hold lifetime (the invoice expiry) are looked
//     at, so a client paying just before expiry is never refunded early;
//   - a deal is only moved to refunded when LNbits itself reports the hold
//     as UNPAID / EXPIRED / CANCELLED — i.e. the network has already stopped
//     holding (and already returned) any funds;
//   - holds LNbits still reports as held or settled are left untouched, and
//     the sweep never pays anything out.
func RunHoldSweep(ctx context.Context, service *Service, interval, holdLifetime time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		sweep(ctx, service, holdLifetime)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sweep(ctx context.Context, service *Service, holdLifetime time.Duration) {
	cutoff := time.Now().Add(-holdLifetime)

	swept, err := service.SweepExpiredHolds(ctx, cutoff)
	if err != nil {
		log.Printf("hold sweep error: %v", err)
		return
	}
	if swept > 0 {
		log.Printf("hold sweep refunded %d expired deal(s)", swept)
	}
}
