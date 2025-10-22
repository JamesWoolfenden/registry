package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	exportData = "scripts/mirror_data/fetch/production_servers.json"
	baseURL    = "https://registry.modelcontextprotocol.io/v0/servers"
	timeout    = 30 * time.Second
	rateLimit  = 100 * time.Millisecond
)

type ServerResponse struct {
	Servers  []json.RawMessage `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor,omitempty"`
		Count      int    `json:"count"`
	} `json:"metadata"`
}

type Fetcher struct {
	client  *http.Client
	baseURL string
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL: baseURL,
	}
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	fetcher := NewFetcher()

	ctx := context.Background()
	allServers, err := fetcher.fetchAll(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to fetch servers")
	}

	if err := saveToFile(allServers, exportData); err != nil {
		log.Fatal().Err(err).Msg("Failed to save data")
	}

	log.Info().Msgf("Successfully saved %d servers to %s", len(allServers), exportData)
}

func (f *Fetcher) fetchAll(ctx context.Context) ([]json.RawMessage, error) {
	var allServers []json.RawMessage
	cursor := ""
	pageCount := 0

	for {
		pageCount++

		servers, nextCursor, err := f.fetchPage(ctx, cursor, pageCount)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", pageCount, err)
		}

		allServers = append(allServers, servers...)
		log.Info().Msgf("Page %d: got %d servers (total: %d)", pageCount, len(servers), len(allServers))

		if nextCursor == "" {
			break
		}
		cursor = nextCursor

		// Rate limiting
		select {
		case <-time.After(rateLimit):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return allServers, nil
}

func (f *Fetcher) fetchPage(ctx context.Context, cursor string, pageNum int) ([]json.RawMessage, string, error) {
	requestURL := f.baseURL
	if cursor != "" {
		requestURL = fmt.Sprintf("%s?cursor=%s", f.baseURL, url.QueryEscape(cursor))
	}

	log.Info().Msgf("Fetching page %d: %s", pageNum, requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)

	if err != nil {
		return nil, "", fmt.Errorf("failed to execute request: %w", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response body: %w", err)
	}

	var serverResp ServerResponse
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, "", fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return serverResp.Servers, serverResp.Metadata.NextCursor, nil
}

func saveToFile(servers []json.RawMessage, filename string) error {
	output := map[string]interface{}{
		"servers": servers,
		"count":   len(servers),
		"fetched": time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// #nosec G306
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}

	return nil
}
