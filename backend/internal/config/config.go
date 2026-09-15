package config

import (
	"log"
	"os"
	"strconv"

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
	}

	return cfg
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
