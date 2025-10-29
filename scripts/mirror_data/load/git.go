package main

import (
	"context"

	"google.golang.org/genai"
)

//nolint:all
func ScanRepo(ctx context.Context, client *genai.Client, model string, mcp Server) (*PaloMeta, error) {
	var test PaloMeta
	return &test, nil
}
