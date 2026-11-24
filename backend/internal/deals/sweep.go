package deals

import (
	"context"
	"log"
	"time"
)

// RunHoldSweep periodically reconciles open deals against LNbits until the
// context is cancelled. It runs immediately, then every interval.
//
// Sweeping is deliberately conservative:
//   - hold invoices can have per-milestone expiries, so sweep checks open deals
//     by LNbits status rather than assuming one global hold lifetime;
//   - a deal is only moved to refunded when LNbits itself reports the hold as
//     UNPAID / EXPIRED / CANCELLED — i.e. the network has already stopped
//     holding (and already returned) any funds;
//   - holds LNbits still reports as held or settled are left untouched, and
//     the sweep never pays anything out.
func RunHoldSweep(ctx context.Context, service *Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		sweep(ctx, service)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sweep(ctx context.Context, service *Service) {
	swept, err := service.SweepExpiredHolds(ctx)
	if err != nil {
		log.Printf("hold sweep error: %v", err)
		return
	}
	if swept > 0 {
		log.Printf("hold sweep refunded %d expired deal(s)", swept)
	}
}
