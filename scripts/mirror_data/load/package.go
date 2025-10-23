package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"path/filepath"
	"strings"

	"io"
	"net/http"
	"os"

	"github.com/rs/zerolog/log"
)

func GetArchiveCode(registryURL string) ([]string, error) {
	codeExtensions := map[string]bool{
		".js": true, ".ts": true, ".cjs": true, ".map": true, ".cts": true,
	}
	resp, err := http.Get(registryURL)

	if err != nil {
		log.Error().Msgf("Failed to fetch npm registry: %v", err.Error())
		return nil, err
	}

	if resp.StatusCode != 200 {
		log.Error().Msgf("Failed to fetch npm registry: %v", resp.StatusCode)
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msgf("Failed to close response body: %v ", err)
		}
	}(resp.Body)

	var meta npmRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		log.Error().Msgf("Failed to decode registry response: %v", err)
		return nil, err
	}

	latestVersion, ok := meta.DistTags["latest"]
	if !ok {
		log.Error().Msgf("Could not find latest version: %v", err)
		return nil, err
	}

	versionInfo, ok := meta.Versions[latestVersion]
	if !ok {
		log.Error().Msgf("Could not find version info: %v", err)
		return nil, err
	}

	tarballURL := versionInfo.Dist.Tarball
	if tarballURL == "" {
		log.Error().Msgf("Could not find tarball URL: %v", err)
		return nil, err
	}

	// Download the tarball
	tarResp, err := http.Get(tarballURL)
	if err != nil {
		log.Error().Msgf("Failed to download tarball: %v ", err)
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Msgf("Failed to close response body: %v ", err)
		}
	}(tarResp.Body)

	// Save tarball to temp file
	tmpFile, err := os.CreateTemp("", "npm-package-*.tgz")
	if err != nil {
		log.Error().Msgf("Failed to create temp file: %v ", err)
		return nil, err
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			log.Error().Msgf("Failed to remove temp file: %v ", err)
		}
	}(tmpFile.Name())

	if _, err := io.Copy(tmpFile, tarResp.Body); err != nil {
		log.Error().Msgf("Failed to save tarball: %v ", err)
		return nil, err
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		log.Error().Msgf("Failed to rewind file: %v ", err)
		return nil, err
	}

	gzr, err := gzip.NewReader(tmpFile)
	if err != nil {
		log.Error().Msgf("Failed to open gzip: %v ", err)
		return nil, err
	}

	defer func(gzr *gzip.Reader) {
		err := gzr.Close()
		if err != nil {
			log.Error().Msgf("Failed to close gzip: %v ", err)
		}
	}(gzr)

	tr := tar.NewReader(gzr)

	var AllCode []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatal().Msgf("failed to get next file %v", err)
		}

		ext := filepath.Ext(header.Name)

		if header.Typeflag == tar.TypeReg && !strings.Contains(header.Name, "._") && codeExtensions[ext] {
			content, err := io.ReadAll(tr)
			if err != nil {
				log.Fatal().Msgf("failed to read: %v", err)
			}

			AllCode = append(AllCode, "// File: "+header.Name+"\n"+string(content))
		}
	}

	return AllCode, nil
}
