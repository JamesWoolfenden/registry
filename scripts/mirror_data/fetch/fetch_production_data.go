package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"
)

type ServerResponse struct {
	Servers  []json.RawMessage `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor,omitempty"`
		Count      int    `json:"count"`
	} `json:"metadata"`
}

type Fetcher struct {
	client    *http.Client
	baseURL   string
	rateLimit time.Duration
}

func NewFetcher(config Config) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: config.Timeout,
		},
		baseURL:   config.BaseURL,
		rateLimit: config.RateLimit,
	}
}

func main() {
	app := &cli.App{
		Name:  "fetch-production-data",
		Usage: "Fetch production data from MCP registry",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "export-data",
				Aliases: []string{"e"},
				Value:   "scripts/mirror_data/fetch/production_servers.json",
				Usage:   "Path to export data file",
			},
			&cli.StringFlag{
				Name:    "base-url",
				Aliases: []string{"u"},
				Value:   "https://registry.modelcontextprotocol.io/v0/servers",
				Usage:   "Base URL for the registry API",
			},
			&cli.DurationFlag{
				Name:    "timeout",
				Aliases: []string{"t"},
				Value:   30 * time.Second,
				Usage:   "Request timeout duration",
			},
			&cli.DurationFlag{
				Name:    "rate-limit",
				Aliases: []string{"r"},
				Value:   100 * time.Millisecond,
				Usage:   "Rate limit between requests",
			},
		},
		Action: func(c *cli.Context) error {
			config := Config{
				ExportData: c.String("export-data"),
				BaseURL:    c.String("base-url"),
				Timeout:    c.Duration("timeout"),
				RateLimit:  c.Duration("rate-limit"),
			}

			return fetchProductionData(config)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

type Config struct {
	ExportData string
	BaseURL    string
	Timeout    time.Duration
	RateLimit  time.Duration
}

func fetchProductionData(config Config) error {
	// Make export data path absolute
	if !filepath.IsAbs(config.ExportData) {
		var err error
		config.ExportData, err = filepath.Abs(config.ExportData)
		if err != nil {
			return fmt.Errorf("failed to convert to absolute path: %w", err)
		}
	}

	fetcher := NewFetcher(config)

	ctx := context.Background()
	allServers, err := fetcher.fetchAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch servers: %w", err)
	}

	if err := saveToFile(allServers, config.ExportData); err != nil {
		return fmt.Errorf("failed to save data: %w", err)
	}

	fmt.Printf("Successfully saved %d servers to %s\n", len(allServers), config.ExportData)
	return nil
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
		fmt.Printf("Page %d: got %d servers (total: %d)\n", pageCount, len(servers), len(allServers))

		if nextCursor == "" {
			break
		}
		cursor = nextCursor

		// Rate limiting
		select {
		case <-time.After(f.rateLimit):
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

	fmt.Printf("Fetching page %d: %s\n", pageNum, requestURL)

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
			fmt.Printf("Warning: Failed to close response body: %v\n", closeErr)
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

	// Ensure filename is an absolute path
	absFilename, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("failed to convert to absolute path: %w", err)
	}

	// #nosec G306
	if err := os.WriteFile(absFilename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}

	return nil
}
