package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

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

//const baseURL = "https://registry.modelcontextprotocol.io/v0/servers"

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	config := loadConfig()

	// Database connection
	db, err := sql.Open("postgres", config.DataSource)

	if err != nil {
		log.Fatal().Msgf("failed to connect to database: %v", err)
	}

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatal().Msgf("Failed to close database: %v", err)
		}
	}(db)

	var maxMigration = 0
	// Run migrations up to a specific point (configure as needed)
	if !(config.SkipMigrations) {
		maxMigration = migrate(db)
	}

	err = importer(db)

	if err != nil {
		log.Fatal().Msgf("failed to import production server data: %v", err)
	}

	verify(db, maxMigration)
}

func verify(db *sql.DB, maxMigration int) {
	// Verify the data
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count)
	if err != nil {
		return
	}
	log.Info().Msgf("\nTotal servers in database: %d\n", count)

	// Check for NULL status values in the JSON
	log.Info().Msg("\nAnalyzing status field in JSON data...")

	rows, err := db.Query(`
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
		log.Fatal().Msgf("failed to analyze data: %v", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal().Msgf("Failed to close rows: %v", err)
		}
	}(rows)

	if rows.Next() {
		var total, nullStatus, emptyStatus, stringNullStatus, activeStatus, deprecatedStatus, deletedStatus int
		err := rows.Scan(&total, &nullStatus, &emptyStatus, &stringNullStatus, &activeStatus, &deprecatedStatus, &deletedStatus)
		if err != nil {
			return
		}

		fmt.Printf("  Total servers: %d\n", total)
		fmt.Printf("  NULL status: %d\n", nullStatus)
		fmt.Printf("  Empty status: %d\n", emptyStatus)
		fmt.Printf("  'null' string status: %d\n", stringNullStatus)
		fmt.Printf("  'active' status: %d\n", activeStatus)
		fmt.Printf("  'deprecated' status: %d\n", deprecatedStatus)
		fmt.Printf("  'deleted' status: %d\n", deletedStatus)
		fmt.Printf("  Other/Invalid: %d\n", total-nullStatus-emptyStatus-stringNullStatus-activeStatus-deprecatedStatus-deletedStatus)
	}

	// Print sample servers with no status
	fmt.Println("\nSample servers with NULL status:")
	rows, err = db.Query(`
		SELECT value->>'name', value->>'version'
		FROM servers
		WHERE value->>'status' IS NULL
		LIMIT 5
	`)
	if err == nil {
		defer func(rows *sql.Rows) {
			err := rows.Close()
			if err != nil {
				log.Fatal().Msgf("failed to close rows:%v", err)
			}
		}(rows)
		for rows.Next() {
			var name, version string
			err := rows.Scan(&name, &version)
			if err != nil {
				return
			}
			log.Info().Msgf("  - %s@%s\n", name, version)
		}
	}

	fmt.Printf("\nDatabase is ready for testing migration %03d!\n", maxMigration+1)
}

func importer(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
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
	stmt, err := tx.Prepare("INSERT INTO servers (version, server_name, published_at, updated_at, is_latest, value) VALUES ($1, $2, $3, $4, $5, $6)")

	if err != nil {
		log.Fatal().Msgf("failed to prepare statement: %v", err)
		return err
	}

	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Info().Msgf("failed to close statement: %v", err)
		}
	}(stmt)
	// Insert each server
	for i, server := range prodData.Servers {

		// Generate a unique version_id
		versionID := uuid.New().String()

		var rawText map[string]interface{}
		if err := json.Unmarshal(server, &rawText); err != nil {
			log.Printf("Failed to unmarshal server %d: %v", i, err)
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

		exists, err := checkExistsInDB(db, serverName, version)
		if err != nil {
			log.Info().Msgf("Failed to check if server exists at index %d: %v", i, err)
			continue
		}

		if exists {
			log.Info().Msgf("skipping duplicate server %s version: %s", serverName, version)
		} else {
			// The server data is already JSON, just insert it
			_, err := stmt.Exec(versionID, serverName, publishedAt, updatedAt, isLatest, value)

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

func checkExists(serverName string, version string) bool {

	config := loadConfig()

	//example
	//https://registry.modelcontextprotocol.io/v0/servers?search=ai.aliengiraffe/spotdb&version=0.1.0
	req, err := http.NewRequest("GET", config.BaseURL, nil)
	if err != nil {
		return false
	}

	q := req.URL.Query()
	q.Add("search", serverName)
	q.Add("version", version)
	req.URL.RawQuery = q.Encode()

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return false
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Info().Msgf("Failed to close body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal().Msgf("Failed to read body: %v", err)
	}
	var serverResp map[string]interface{}

	if err := json.Unmarshal(body, &serverResp); err != nil {
		log.Fatal().Msgf("Failed to parse JSON: %v", err)
	}

	//if it's found more than no servers it does exist
	if len(serverResp["servers"].([]interface{})) > 0 {
		return true
	}

	return false
}

func migrate(db *sql.DB) int {
	maxMigration := 7 // Change this to test different migration states

	log.Info().Msgf("Running migrations 001-%03d...\n", maxMigration)

	migrationsDir := "internal/database/migrations"
	for i := 1; i <= maxMigration; i++ {
		migrationFile := filepath.Join(migrationsDir, fmt.Sprintf("%03d_*.sql", i))
		files, err := filepath.Glob(migrationFile)

		if err != nil || len(files) == 0 {
			log.Fatal().Msgf("Migration file %d not found", i)
		}

		content, err := os.ReadFile(files[0])
		if err != nil {
			log.Fatal().Msgf("Failed to read migration %d: %v", i, err)
		}

		fmt.Printf("  Applying migration %d: %s\n", i, filepath.Base(files[0]))
		if _, err := db.Exec(string(content)); err != nil {
			log.Fatal().Msgf("Failed to apply migration %d: %v", i, err)
		}
	}
	return maxMigration
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

func checkExistsInDB(db *sql.DB, serverName, version string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM servers WHERE server_name = $1 AND value->>'version' = $2",
		serverName, version).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
