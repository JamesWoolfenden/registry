package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// Updated migrate function with context
func migrateWithContext(ctx context.Context, db *sql.DB) (int, error) {
	maxMigration := 7 // Change this to test different migration states

	log.Info().Msgf("Running migrations 001-%03d...", maxMigration)

	migrationsDir := "internal/database/migrations"
	for i := 1; i <= maxMigration; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return i - 1, ctx.Err()
		default:
		}

		migrationFile := filepath.Join(migrationsDir, fmt.Sprintf("%03d_*.sql", i))
		files, err := filepath.Glob(migrationFile)

		if err != nil || len(files) == 0 {
			return i - 1, fmt.Errorf("migration file %d not found", i)
		}

		content, err := os.ReadFile(files[0])
		if err != nil {
			return i - 1, fmt.Errorf("failed to read migration %d: %w", i, err)
		}

		log.Info().Msgf("  Applying migration %d: %s", i, filepath.Base(files[0]))
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			return i - 1, fmt.Errorf("failed to apply migration %d: %w", i, err)
		}
	}
	return maxMigration, nil
}
