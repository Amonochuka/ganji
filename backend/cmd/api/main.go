package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/db"
)

func main() {
	cfg := config.Load()

	dbConn, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to set up database: %v", err)
	}
	defer dbConn.Close()

	router := setupRouter(cfg, dbConn)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// run server in the background so we can listen for shutdown signals
	go func() {
		log.Printf("ganji backend listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed to start: %v", err)
		}
	}()

	// wait for SIGTERM (Render/Railway redeploy) or SIGINT (local Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("shutting down server...")

	// give in-flight requests 30 seconds to complete — important here
	// specifically because a request could be mid-escrow-release when a
	// deploy happens, and we never want to abruptly cut that off
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server stopped cleanly")
}