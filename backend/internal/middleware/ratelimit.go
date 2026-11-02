package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // requests per window
	window   time.Duration // time window
}

type visitor struct {
	tokens     int
	lastRefill time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, v := range rl.visitors {
			if now.Sub(v.lastRefill) > 10*time.Minute {
				delete(rl.visitors, key)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, exists := rl.visitors[key]
	if !exists {
		rl.visitors[key] = &visitor{
			tokens:     rl.rate - 1,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(v.lastRefill)
	tokensToAdd := int(elapsed / (rl.window / time.Duration(rl.rate)))
	if tokensToAdd > 0 {
		v.tokens += tokensToAdd
		if v.tokens > rl.rate {
			v.tokens = rl.rate
		}
		v.lastRefill = now
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}
	return false
}

// RateLimit creates a rate limiting middleware.
// keyFunc determines the key for rate limiting (e.g., IP address, user ID).
func RateLimit(rate int, window time.Duration, keyFunc func(*gin.Context) string) gin.HandlerFunc {
	if rate <= 0 {
		rate = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	limiter := newRateLimiter(rate, window)

	return func(c *gin.Context) {
		key := keyFunc(c)
		if key == "" {
			key = c.ClientIP()
		}
		if !limiter.allow(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RateLimitByIP limits requests by client IP address
func RateLimitByIP(rate int, window time.Duration) gin.HandlerFunc {
	return RateLimit(rate, window, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

// RateLimitAuth limits authentication attempts (stricter)
func RateLimitAuth() gin.HandlerFunc {
	return RateLimitByIP(5, time.Minute)
}

// RateLimitPublic limits public endpoints
func RateLimitPublic() gin.HandlerFunc {
	return RateLimitByIP(60, time.Minute)
}
