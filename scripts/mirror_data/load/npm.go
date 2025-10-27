package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

const npmRegistry = "https://registry.npmjs.org"

func ScanNpmPackages(ctx context.Context, client *genai.Client, model string, mcp Server) (*PaloMeta, error) {
	query := fmt.Sprint(npmRegistry, "/%s")

	if len(mcp.Packages) == 0 {
		return nil, fmt.Errorf("no packages found")
	}

	packageName := mcp.Packages[0].Identifier
	if packageName == "" {
		return nil, fmt.Errorf("package name %s is empty", packageName)
	}

	registryURL := fmt.Sprintf(query, mcp.Packages[0].Identifier)
	log.Info().Str("url", registryURL).Msg("Scanning package")

	AllCode, err := GetArchiveCode(registryURL)

	if err != nil {
		log.Error().Msgf("Error expanding archive: %v", err)
		return nil, err
	}

	Palo, err := Dispatch(ctx, AllCode, client, model)
	if err != nil {
		log.Error().Msgf("Error dispatching library: %v", err)
		return nil, err
	}

	return Palo, nil
}
