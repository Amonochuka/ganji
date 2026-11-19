package middleware

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewCORSMiddleware(allowedOrigins []string, allowPrivateLAN bool) gin.HandlerFunc {
	origins := append([]string(nil), allowedOrigins...)

	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			for _, allowed := range origins {
				if origin == allowed {
					return true
				}
			}
			return allowPrivateLAN && isPrivateLANDevOrigin(origin)
		},
		AllowMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:        []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials:    true,
		AllowPrivateNetwork: true,
		MaxAge:              12 * time.Hour,
	})
}

func isPrivateLANDevOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Port() != "3000" {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".local") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsPrivate()
}
