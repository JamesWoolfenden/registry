// This tool was created by Claude Code as a simple way to kick the tires on data migrations
// by loading production data into a test database for migration testing.
// It is not intended for production use.
//

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

const exportData = "scripts/mirror_data/fetch/production_servers.json"
const skipMigrations = true
const dataSource="postgres://mcpregistry:mcpregistry@localhost:5432/mcp-registry?sslmode=disable"


func main() {
	// Database connection
	db, err := sql.Open("postgres", dataSource)

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatal("Failed to close database:", err)
		}
	}(db)

	var maxMigration = 0
	// Run migrations up to a specific point (configure as needed)
	if !(skipMigrations) {
		maxMigration = migrate(db)
	}

	err = importer(db)

	if err != nil {
		log.Fatal("failed to import production server data:", err)
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
	fmt.Printf("\nTotal servers in database: %d\n", count)

	// Check for NULL status values in the JSON
	fmt.Println("\nAnalyzing status field in JSON data...")

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
		log.Fatal("Failed to analyze data:", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal("Failed to close rows:", err)
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
				log.Fatal("Failed to close rows:", err)
			}
		}(rows)
		for rows.Next() {
			var name, version string
			err := rows.Scan(&name, &version)
			if err != nil {
				return
			}
			fmt.Printf("  - %s@%s\n", name, version)
		}
	}

	fmt.Printf("\nDatabase is ready for testing migration %03d!\n", maxMigration+1)
}

func importer(db *sql.DB) error {
	// Load production data
	fmt.Println("\nLoading production data...")

	data, err := os.ReadFile(exportData)

	if err != nil {
		log.Printf("failed to read export file: %v", err)
		return err
	}

	var prodData struct {
		Servers []json.RawMessage `json:"servers"`
	}

	if err := json.Unmarshal(data, &prodData); err != nil {
		log.Printf("failed to parse export file: %v", err)
		return err
	}

	fmt.Printf("Loading %d servers...\n", len(prodData.Servers))

	// Prepare insert statement
	stmt, err := db.Prepare("INSERT INTO servers (version, server_name, published_at, updated_at, is_latest, value) VALUES ($1, $2, $3, $4, $5, $6)")

	if err != nil {
		log.Fatal("Failed to prepare statement:", err)
		return err
	}

	defer func(stmt *sql.Stmt) {
		err := stmt.Close()
		if err != nil {
			log.Printf("failed to close statement: %v", err)
		}
	}(stmt)

	// Insert each server
	for i, server := range prodData.Servers {
		// Generate a unique version_id
		versionID := uuid.New().String()

		var rawText map[string]interface{}

		err := json.Unmarshal(server, &rawText)
		if err != nil {
			return err
		}

		parsed := rawText["server"].(map[string]interface{})
		serverName := parsed["name"].(string)

		meta := rawText["_meta"].(map[string]interface{})
		mcp:= meta["io.modelcontextprotocol.registry/official"].(map[string]interface{})

		publishedAt := mcp["publishedAt"]
		updatedAt := mcp["updatedAt"]
		isLatest := mcp["isLatest"]

		// The server data is already JSON, just insert it
		if _, err := stmt.Exec(versionID, serverName, publishedAt, updatedAt, isLatest, server); err != nil {
			log.Printf("Failed to insert server %d: %v", i, err)
			continue
		}
		log.Printf("Inserted server %d: %v", i, serverName)
	}

	fmt.Println("Data loaded successfully!")
	return nil
}

func migrate(db *sql.DB) int {
	maxMigration := 7 // Change this to test different migration states

	fmt.Printf("Running migrations 001-%03d...\n", maxMigration)

	migrationsDir := "internal/database/migrations"
	for i := 1; i <= maxMigration; i++ {
		migrationFile := filepath.Join(migrationsDir, fmt.Sprintf("%03d_*.sql", i))
		files, err := filepath.Glob(migrationFile)

		if err != nil || len(files) == 0 {
			log.Fatalf("Migration file %d not found", i)
		}

		content, err := os.ReadFile(files[0])
		if err != nil {
			log.Fatalf("Failed to read migration %d: %v", i, err)
		}

		fmt.Printf("  Applying migration %d: %s\n", i, filepath.Base(files[0]))
		if _, err := db.Exec(string(content)); err != nil {
			log.Fatalf("Failed to apply migration %d: %v", i, err)
		}
	}
	return maxMigration
}
