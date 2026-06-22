package db

import (
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations applies all pending migrations from backend/migrations.
// Safe to run on every boot — golang-migrate tracks which migrations have
// already been applied and skips them.
func RunMigrations(databaseURL string) error {
	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			log.Println("no new migrations to apply")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	log.Println("migrations applied successfully")
	return nil
}
