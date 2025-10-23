package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

func ScanNpmPackages(ctx context.Context, client *genai.Client, model string, mcp Server) (*PaloMeta, error) {
	registryURL := fmt.Sprintf("https://registry.npmjs.org/%s", mcp.Packages[0].Identifier)

	fmt.Println("registryURL:", registryURL)
	AllCode, err := GetArchiveCode(registryURL)

	if err != nil {
		log.Error().Msgf("Error expanding archive: %v", err)
		return nil, err
	}

	Palo, err := Dispatch(AllCode, ctx, client, model)
	if err != nil {
		log.Error().Msgf("Error dispatching library: %v", err)
		return nil, err
	}

	return Palo, nil
}
