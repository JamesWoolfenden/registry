package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Config struct {
	ExportDataPath string
	SkipMigrations bool
	DataSource     string
	BaseURL        string
	MaxMigration   int
}

// Configuration helpers
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func loadConfig() *Config {
	return &Config{
		ExportDataPath: getEnv("EXPORT_DATA_PATH", "scripts/mirror_data/fetch/production_servers.json"),
		SkipMigrations: getEnvBool("SKIP_MIGRATIONS", true),
		DataSource:     getEnv("DATABASE_URL", "postgres://mcpregistry:mcpregistry@localhost:5432/mcp-registry?sslmode=disable"),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080/v0/servers"),
		MaxMigration:   getEnvInt("MAX_MIGRATION", 7),
	}
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Create context with timeout
	err := transaction()
	if err != nil {
		log.Fatal().Err(err).Msg("load failed")
	}
}

func transaction() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	config := loadConfig()

	// Database connection
	db, err := sql.Open("postgres", config.DataSource)

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Error().Msgf("Failed to close database: %v", err)
		}
	}(db)

	var maxMigration = 0
	// Run migrations up to a specific point (configure as needed)
	if !(config.SkipMigrations) {
		maxMigration, err = migrateWithContext(ctx, db)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	err = importerWithContext(ctx, db)

	if err != nil {
		return fmt.Errorf("failed to import production server data: %w", err)
	}

	err = verifyWithContext(ctx, db, maxMigration)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	return nil
}

// Updated database helpers with context
func checkExistsInDB(ctx context.Context, db *sql.DB, serverName, version string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM servers 
		WHERE server_name = $1 AND value->>'version' = $2
	`, serverName, version).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check if server exists: %w", err)
	}
	return count > 0, nil
}

// Updated verify function with context
func verifyWithContext(ctx context.Context, db *sql.DB, maxMigration int) error {
	// Verify the data
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM servers").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count servers: %w", err)
	}
	log.Info().Msgf("\nTotal servers in database: %d\n", count)

	// Check for NULL status values in the JSON with context
	log.Info().Msg("Analyzing status field in JSON data...")

	rows, err := db.QueryContext(ctx, `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN value->>'status' IS NULL THEN 1 END) as null_status,
			COUNT(CASE WHEN value->>'status' = '' THEN 1 END) as empty_status,
			COUNT(CASE WHEN value->>'status' = 'null' THEN 1 END) as string_null_status,
			COUNT(CASE WHEN value->>'status' = 'active' THEN 1 END) as active_status,
			COUNT(CASE WHEN value->>'status' = 'deprecated' THEN 1 END) as deprecated_status,
			COUNT(CASE WHEN value->>'status' = 'deleted' THEN 1 END) as deleted_status
		FROM servers
	`)

	if err != nil {
		return fmt.Errorf("failed to analyze data: %w", err)
	}

	defer rows.Close()

	if rows.Err() != nil {
		return fmt.Errorf("failed to analyze data: %w", rows.Err())
	}

	if rows.Next() {
		var total, nullStatus, emptyStatus, stringNullStatus, activeStatus, deprecatedStatus, deletedStatus int
		err := rows.Scan(&total, &nullStatus, &emptyStatus, &stringNullStatus, &activeStatus, &deprecatedStatus, &deletedStatus)
		if err != nil {
			return fmt.Errorf("failed to scan analysis results: %w", err)
		}

		log.Info().Msgf("  Total servers: %d", total)
		log.Info().Msgf("  NULL status: %d", nullStatus)
		log.Info().Msgf("  Empty status: %d", emptyStatus)
		log.Info().Msgf("  'null' string status: %d", stringNullStatus)
		log.Info().Msgf("  'active' status: %d", activeStatus)
		log.Info().Msgf("  'deprecated' status: %d", deprecatedStatus)
		log.Info().Msgf("  'deleted' status: %d", deletedStatus)
		log.Info().Msgf("  Other/Invalid: %d", total-nullStatus-emptyStatus-stringNullStatus-activeStatus-deprecatedStatus-deletedStatus)
	}

	// Print sample servers with no status using context
	log.Info().Msg("Sample servers with NULL status:")
	rows, err = db.QueryContext(ctx, `
		SELECT value->>'name', value->>'version'
		FROM servers
		WHERE value->>'status' IS NULL
		LIMIT 5
	`)

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, version string
			err := rows.Scan(&name, &version)
			if err != nil {
				return fmt.Errorf("failed to scan sample data: %w", err)
			}
			log.Info().Msgf("  - %s@%s", name, version)
		}
	}

	defer rows.Close()

	if rows.Err() != nil {
		return fmt.Errorf("failed to analyze data: %w", err)
	}

	log.Info().Msgf("Database is ready for testing migration %03d!\n", maxMigration+1)
	return nil
}

// Updated importer with context
func importerWithContext(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	config := loadConfig()

	// Load production data
	log.Info().Msg("\nLoading production data...")

	data, err := os.ReadFile(config.ExportDataPath)

	if err != nil {
		log.Printf("failed to read export file: %v", err)
		return err
	}

	var prodData struct {
		Servers []json.RawMessage `json:"servers"`
	}

	if err := json.Unmarshal(data, &prodData); err != nil {
		log.Info().Msgf("failed to parse export file: %v", err)
		return err
	}

	log.Info().Msgf("Loading %d servers...\n", len(prodData.Servers))

	// Prepare insert statement
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO servers (version, server_name, published_at, updated_at, is_latest, value) VALUES ($1, $2, $3, $4, $5, $6)")

	if err != nil {
		log.Error().Msgf("failed to prepare statement: %v", err)
		return err
	}

	defer stmt.Close()

	// Insert each server
	for i, server := range prodData.Servers {
		// Generate a unique version_id
		versionID := uuid.New().String()

		var rawText map[string]interface{}
		if err := json.Unmarshal(server, &rawText); err != nil {
			log.Info().Msgf("Failed to unmarshal server %d: %v", i, err)
			continue
		}

		parsed, err := safeMapFromMap(rawText, "server")
		if err != nil {
			log.Info().Msgf("Invalid server structure at index %d: %v", i, err)
			continue
		}

		value, err := json.Marshal(parsed)

		if err != nil {
			return err
		}

		serverName, err := safeStringFromMap(parsed, "name")
		if err != nil {
			log.Info().Msgf("Missing server name at index %d: %v", i, err)
			continue
		}

		version, err := safeStringFromMap(parsed, "version")
		if err != nil {
			log.Info().Msgf("Missing server version at index %d: %v", i, err)
			continue
		}

		meta := rawText["_meta"].(map[string]interface{})
		mcp := meta["io.modelcontextprotocol.registry/official"].(map[string]interface{})

		publishedAt := mcp["publishedAt"]
		updatedAt := mcp["updatedAt"]
		isLatest := mcp["isLatest"]

		exists, err := checkExistsInDB(ctx, db, serverName, version)
		if err != nil {
			log.Info().Msgf("Failed to check if server exists at index %d: %v", i, err)
			continue
		}

		if exists {
			log.Info().Msgf("skipping duplicate server %s version: %s", serverName, version)
		} else {
			// The server data is already JSON, just insert it
			_, err := stmt.ExecContext(ctx, versionID, serverName, publishedAt, updatedAt, isLatest, value)

			if err != nil {
				log.Info().Msgf("Failed to insert server %d: %v", i, err)
				continue
			}

			log.Info().Msgf("Inserted server %d: %v", i, serverName)
		}
	}

	log.Info().Msg("Data loaded successfully!")
	return tx.Commit()
}

// Updated migrate function with context
func migrateWithContext(ctx context.Context, db *sql.DB) (int, error) {
	maxMigration := 7 // Change this to test different migration states

	log.Info().Msgf("Running migrations 001-%03d...\n", maxMigration)

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

		log.Info().Msgf("  Applying migration %d: %s\n", i, filepath.Base(files[0]))
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			return i - 1, fmt.Errorf("failed to apply migration %d: %w", i, err)
		}
	}
	return maxMigration, nil
}

func safeStringFromMap(m map[string]interface{}, key string) (string, error) {
	val, exists := m[key]
	if !exists {
		return "", fmt.Errorf("key %s not found", key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("key %s is not a string", key)
	}
	return str, nil
}

func safeMapFromMap(m map[string]interface{}, key string) (map[string]interface{}, error) {
	val, exists := m[key]
	if !exists {
		return nil, fmt.Errorf("key %s not found", key)
	}
	mapVal, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key %s is not a map", key)
	}
	return mapVal, nil
}
