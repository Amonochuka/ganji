package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                     string
	DatabaseURL              string
	JWTSecret                string
	JWTRefreshSecret         string
	LNBitsURL                string
	LNBitsAPIKey             string
	LNBitsAdminKey           string
	LNBitsWebhookSecret      string
	WebhookURL               string
	FrontendURL              string
	HoldInvoiceExpirySeconds int64
	HoldSweepIntervalSeconds int64
	StoragePath              string
	MaxUploadBytes           int64
	OperatorEmails           []string
	SMTPHost                 string
	SMTPPort                 int
	SMTPUser                 string
	SMTPPass                 string
	SMTPFrom                 string
	SMTPEncryption           string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading from real environment variables")
	}

	cfg := &Config{
		Port:                     getEnv("PORT", "8080"),
		DatabaseURL:              requireEnv("DATABASE_URL"),
		JWTSecret:                requireEnv("JWT_SECRET"),
		JWTRefreshSecret:         requireEnv("JWT_REFRESH_SECRET"),
		LNBitsURL:                getEnv("LNBITS_URL", ""),
		LNBitsAPIKey:             getEnv("LNBITS_API_KEY", ""),
		LNBitsAdminKey:           getEnv("LNBITS_ADMIN_KEY", ""),
		LNBitsWebhookSecret:      getEnv("LNBITS_WEBHOOK_SECRET", ""),
		WebhookURL:               getEnv("WEBHOOK_URL", ""),
		FrontendURL:              getEnv("FRONTEND_URL", "http://localhost:3000"),
		HoldInvoiceExpirySeconds: getEnvInt("LNBITS_HOLD_INVOICE_EXPIRY_SECONDS", 2_592_000),
		HoldSweepIntervalSeconds: getEnvInt("LNBITS_HOLD_SWEEP_INTERVAL_SECONDS", 21_600),
		StoragePath:              getEnv("STORAGE_PATH", "./uploads"),
		MaxUploadBytes:           getEnvInt("MAX_UPLOAD_BYTES", 10*1024*1024),
		OperatorEmails:           getEnvList("OPERATOR_EMAILS"),
		SMTPHost:                 getEnv("SMTP_HOST", ""),
		SMTPPort:                 int(getEnvInt("SMTP_PORT", 587)),
		SMTPUser:                 getEnv("SMTP_USER", ""),
		SMTPPass:                 getEnv("SMTP_PASS", ""),
		SMTPFrom:                 getEnv("SMTP_FROM", "noreply@ganji.local"),
		SMTPEncryption:           getEnv("SMTP_ENCRYPTION", "starttls"),
	}

	return cfg
}

// getEnvList parses a comma-separated env var into a list of trimmed,
// non-empty entries (e.g. "a@x.com, b@y.com").
func getEnvList(key string) []string {
	var out []string
	for _, part := range strings.Split(os.Getenv(key), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getEnvInt(key string, fallback int64) int64 {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		log.Printf("invalid integer for %s (%q), using fallback %d", key, val, fallback)
		return fallback
	}
	return parsed
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func requireEnv(key string) string {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		log.Fatalf("missing required environment variable: %s", key)
	}
	return val
}
