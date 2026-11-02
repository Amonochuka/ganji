package auth

import (
	"context"
	"log"
	"time"
)

// CleanupExpiredRefreshTokens removes expired and revoked refresh tokens.
func (s *Service) CleanupExpiredRefreshTokens(ctx context.Context) (int64, error) {
	return s.repo.CleanupExpiredRefreshTokens(ctx)
}

// RunRefreshTokenCleanup periodically removes expired and revoked refresh
// tokens until ctx is cancelled. The first cleanup fires immediately, then
// every interval.
func RunRefreshTokenCleanup(ctx context.Context, service *Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		deleted, err := service.CleanupExpiredRefreshTokens(runCtx)
		cancel()
		if err != nil {
			log.Printf("refresh-token cleanup error: %v", err)
		} else if deleted > 0 {
			log.Printf("refresh-token cleanup removed %d row(s)", deleted)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
