// This tool was created by Claude Code as a simple way to kick the tires on data migrations
// by fetching production data from the public registry API.
// It is not intended for production use.
//

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const exportData = "scripts/mirror_data/fetch/production_servers.json"
const baseURL = "https://registry.modelcontextprotocol.io/v0/servers"

type ServerResponse struct {
	Servers  []json.RawMessage `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor,omitempty"`
		Count      int    `json:"count"`
	} `json:"metadata"`
}

func main() {

	var allServers []json.RawMessage
	cursor := ""
	pageCount := 0

	for {
		pageCount++
		url := baseURL

		if cursor != "" {
			url = fmt.Sprintf("%s?cursor=%s", baseURL, cursor)
		}

		fmt.Printf("Fetching page %d: %s\n", pageCount, url)

		resp, err := http.Get(url)

		if err != nil {
			log.Fatalf("Failed to fetch: %v", err)
		}

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Printf("Failed to close body: %v", err)
			}
		}(resp.Body)

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("Failed to read body: %v", err)
		}

		var serverResp ServerResponse

		if err := json.Unmarshal(body, &serverResp); err != nil {
			log.Fatalf("Failed to parse JSON: %v", err)
		}

		allServers = append(allServers, serverResp.Servers...)
		fmt.Printf("  Got %d servers (total: %d)\n", len(serverResp.Servers), len(allServers))

		if serverResp.Metadata.NextCursor == "" {
			break
		}
		cursor = serverResp.Metadata.NextCursor

		// Be nice to the API
		time.Sleep(100 * time.Millisecond)
	}

	// Save all servers to a file
	output := map[string]interface{}{
		"servers": allServers,
		"count":   len(allServers),
		"fetched": time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	if err := os.WriteFile(exportData, data, 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("\nDone! Saved %d servers to %s\n", len(allServers), exportData)
}
