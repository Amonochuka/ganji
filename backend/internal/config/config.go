package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
	JWTRefreshSecret    string
	LNBitsURL           string
	LNBitsAPIKey        string
	LNBitsWebhookSecret string
	FrontendURL         string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, reading from real environment variables")
	}

	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		DatabaseURL:         requireEnv("DATABASE_URL"),
		JWTSecret:           requireEnv("JWT_SECRET"),
		JWTRefreshSecret:    requireEnv("JWT_REFRESH_SECRET"),
		LNBitsURL:           getEnv("LNBITS_URL", ""),
		LNBitsAPIKey:        getEnv("LNBITS_API_KEY", ""),
		LNBitsWebhookSecret: getEnv("LNBITS_WEBHOOK_SECRET", ""),
		FrontendURL:         getEnv("FRONTEND_URL", "http://localhost:3000"),
	}

	return cfg
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