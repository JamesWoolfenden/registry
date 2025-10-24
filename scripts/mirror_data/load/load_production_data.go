package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

type Config struct {
	ExportDataPath string
	SkipMigrations bool
	DataSource     string
	BaseURL        string
	MaxMigration   int
	GCPProject     string
}

func loadConfig() *Config {
	return &Config{
		ExportDataPath: getEnv("EXPORT_DATA_PATH", "scripts/mirror_data/fetch/production_servers.json"),
		SkipMigrations: getEnvBool("SKIP_MIGRATIONS", true),
		DataSource:     getEnv("DATABASE_URL", "postgres://mcpregistry:mcpregistry@localhost:5432/mcp-registry?sslmode=disable"),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080/v0/servers"),
		MaxMigration:   getEnvInt("MAX_MIGRATION", 7),
		GCPProject:     getEnv("GCP_PROJECT", "ocpoit"),
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

// Usage in main transaction function
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
		if closeErr := db.Close(); closeErr != nil {
			log.Error().Msgf("Failed to close database: %v", closeErr)
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

	// Use batched importer instead of single transaction
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
func checkExistsInDB(ctx context.Context, db *sql.DB, serverName, version string) (bool, bool, error) {
	var count int
	var isLatest sql.NullBool

	err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) as count,
			BOOL_OR(is_latest) as is_latest
		FROM servers
		WHERE server_name = $1 AND value->>'version' = $2
	`, serverName, version).Scan(&count, &isLatest)

	if err != nil {
		return false, false, fmt.Errorf("failed to check if server exists: %w", err)
	}

	exists := count > 0
	latest := isLatest.Valid && isLatest.Bool

	return exists, latest, nil
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

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Error().Msgf("Failed to close rows: %v", err)
		}
	}(rows)

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
		defer func(rows *sql.Rows) {
			err := rows.Close()
			if err != nil {
				log.Error().Msgf("Failed to close rows: %v", err)
			}
		}(rows)
		for rows.Next() {
			var name, version string
			err := rows.Scan(&name, &version)
			if err != nil {
				return fmt.Errorf("failed to scan sample data: %w", err)
			}
			log.Info().Msgf("  - %s@%s", name, version)
		}
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Error().Msgf("failed to close rows: %v", err)
		}
	}(rows)

	if rows.Err() != nil {
		return fmt.Errorf("failed to analyze data: %w", err)
	}

	log.Info().Msgf("Database is ready for testing migration %03d!\n", maxMigration+1)
	return nil
}

// Updated importer with batched transactions
func importerWithContext(ctx context.Context, db *sql.DB) error {
	config := loadConfig()

	// Load production data
	log.Info().Msg("\nLoading production data...")

	data, err := os.ReadFile(config.ExportDataPath)
	if err != nil {
		return fmt.Errorf("failed to read export file: %w", err)
	}

	var prodData struct {
		Servers []json.RawMessage `json:"servers"`
	}

	if err := json.Unmarshal(data, &prodData); err != nil {
		return fmt.Errorf("failed to parse export file: %w", err)
	}

	log.Info().Msgf("Loading %d servers in batches of 30...\n", len(prodData.Servers))

	const batchSize = 30
	totalServers := len(prodData.Servers)

	for batchStart := 0; batchStart < totalServers; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > totalServers {
			batchEnd = totalServers
		}

		batch := prodData.Servers[batchStart:batchEnd]
		batchNum := (batchStart / batchSize) + 1
		totalBatches := (totalServers + batchSize - 1) / batchSize

		log.Info().Msgf("Processing batch %d/%d (%d records)...", batchNum, totalBatches, len(batch))

		err := processBatch(ctx, db, batch, batchStart)
		if err != nil {
			log.Error().Msgf("Failed to process batch %d: %v", batchNum, err)
			// Continue with next batch instead of failing completely
			continue
		}

		log.Info().Msgf("Successfully processed batch %d/%d", batchNum, totalBatches)
	}

	log.Info().Msg("Data loading completed!")
	return nil
}

// Process a single batch of servers in a transaction
func processBatch(ctx context.Context, db *sql.DB, batch []json.RawMessage, startIndex int) error {
	// Start transaction for this batch
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Error().Msgf("Failed to rollback batch transaction: %v", rbErr)
			}
		}
	}()

	// Prepare insert statement for this batch
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO servers (version, server_name, published_at, updated_at, is_latest, value, faun) VALUES ($1, $2, $3, $4, $5, $6, $7)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Error().Msgf("Failed to close batch statement: %v", closeErr)
		}
	}()

	// Process each server in the batch
	processedCount := 0
	skippedCount := 0
	errorCount := 0

	for i, server := range batch {
		globalIndex := startIndex + i

		err := processServerRecord(ctx, db, tx, stmt, server, globalIndex)
		if err != nil {
			log.Warn().Msgf("Failed to process server %d: %v", globalIndex, err)
			errorCount++
			continue
		}
		processedCount++
	}

	// Commit the batch transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	log.Info().Msgf("Batch completed: %d processed, %d skipped, %d errors",
		processedCount, skippedCount, errorCount)

	return nil
}

// Process a single server record
func processServerRecord(ctx context.Context, db *sql.DB, tx *sql.Tx, stmt *sql.Stmt, server json.RawMessage, index int) error {
	var rawText map[string]interface{}
	if err := json.Unmarshal(server, &rawText); err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}

	parsed, err := safeMapFromMap(rawText, "server")
	if err != nil {
		return fmt.Errorf("invalid server structure: %w", err)
	}

	value, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("failed to marshal server data: %w", err)
	}

	serverName, err := safeStringFromMap(parsed, "name")
	if err != nil {
		return fmt.Errorf("missing server name: %w", err)
	}

	version, err := safeStringFromMap(parsed, "version")
	if err != nil {
		return fmt.Errorf("missing server version: %w", err)
	}

	meta := rawText["_meta"].(map[string]interface{})
	mcp := meta["io.modelcontextprotocol.registry/official"].(map[string]interface{})

	publishedAt := mcp["publishedAt"]
	updatedAt := mcp["updatedAt"]
	isLatest := mcp["isLatest"]

	exists, latestInDB, err := checkExistsInDB(ctx, db, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to check if server exists: %w", err)
	}

	if exists {
		if latestInDB != isLatest {
			// Update is_latest field only using the transaction
			_, err = tx.ExecContext(ctx, `
				UPDATE servers
				SET is_latest = $1, updated_at = NOW()
				WHERE server_name = $2 AND value->>'version' = $3
			`, isLatest, serverName, version)

			if err != nil {
				return fmt.Errorf("failed to update is_latest: %w", err)
			}

			log.Info().Msgf("Updated is_latest to %v for server %s@%s", isLatest, serverName, version)
		}

		log.Debug().Msgf("Skipping duplicate server %s@%s", serverName, version)
		return nil // Not an error, just skipped
	}

	// Generate review for new server
	paloMeta, err := review(ctx, value)
	if err != nil {
		return fmt.Errorf("failed to generate review: %w", err)
	}

	// Insert new server
	_, err = stmt.ExecContext(ctx, version, serverName, publishedAt, updatedAt, isLatest, value, paloMeta)
	if err != nil {
		return fmt.Errorf("failed to insert server: %w", err)
	}

	log.Info().Msgf("Inserted server %d: %s@%s", index, serverName, version)
	return nil
}

func review(ctx context.Context, value []uint8) ([]byte, error) {
	const model = "gemini-2.5-flash"
	config := loadConfig()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  config.GCPProject,
		Location: "us-central1",
		Backend:  genai.BackendVertexAI,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	var paloMeta *PaloMeta

	var mcp Server

	if err := json.Unmarshal(value, &mcp); err != nil {
		return nil, err
	}

	if len(mcp.Packages) > 0 {
		first := mcp.Packages[0]
		switch first.RegistryType {

		case "npm":
			{
				paloMeta, err = ScanNpmPackages(ctx, client, model, mcp)
				if err != nil {
					log.Info().Msgf("Failed to scan NPM packages: %v", err)
					break
				}
			}
		case "oci":
			{
				//docker?
			}
		}
	}

	if paloMeta == nil {
		if mcp.Repository.Source == "github" {
			log.Info().Msgf("Skipping review for GitHub repository")
			//ReviewGithub()
		} else {
			paloMeta = &PaloMeta{
				PaloExtensions{Score: -999, Review: "This Server has no packages or code available for review"}}
		}
	}

	paloMetaJson, err := json.Marshal(paloMeta)

	if err != nil {
		return nil, err
	}

	return paloMetaJson, nil
}
