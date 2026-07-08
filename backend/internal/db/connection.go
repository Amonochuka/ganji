package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// Open opens a connection pool to PostgreSQL, retries until it's reachable,
// and runs all pending migrations before returning. Caller decides what to
// do on error.
func Open(databaseURL string) (*sql.DB, error) {
	dbConn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	dbConn.SetMaxOpenConns(25)
	dbConn.SetMaxIdleConns(25)
	dbConn.SetConnMaxLifetime(5 * time.Minute)

	if err := pingWithRetry(dbConn, 5); err != nil {
		return nil, fmt.Errorf("database unreachable after retries: %w", err)
	}

	log.Println("connected to database")

	if err := RunMigrations(databaseURL); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return dbConn, nil
}

func pingWithRetry(dbConn *sql.DB, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = dbConn.Ping(); err == nil {
			return nil
		}
		log.Printf("database not ready yet, retrying (%d/%d)...", i+1, attempts)
		time.Sleep(2 * time.Second)
	}
	return err
}
