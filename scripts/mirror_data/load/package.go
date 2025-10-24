package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

func GetArchiveCode(registryURL string) ([]string, error) {
	codeExtensions := map[string]bool{
		".js": true, ".ts": true, ".cjs": true, ".map": true, ".cts": true,
	}

	resp, err := http.Get(registryURL) //nolint:all

	if err != nil {
		log.Error().Msgf("Failed to fetch npm registry: %v", err.Error())
		return nil, err
	}

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Error().Msgf("Failed to close response body: %v ", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch npm registry: %v", resp.StatusCode)
	}

	var meta npmRegistryResponse

	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	latestVersion, ok := meta.DistTags["latest"]
	if !ok {
		return nil, fmt.Errorf("could not find latest version: %s", latestVersion)
	}

	versionInfo, ok := meta.Versions[latestVersion]
	if !ok {
		return nil, fmt.Errorf("could not find version info for latest version: %s", latestVersion)
	}

	tarballURL := versionInfo.Dist.Tarball
	if tarballURL == "" {
		return nil, fmt.Errorf("tarball URL cannot be empty")
	}

	tr, err := getPackageCode(tarballURL)

	if err != nil {
		return nil, err
	}

	var AllCode []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Error().Msgf("failed to get next file %v", err)
			continue
		}

		ext := filepath.Ext(header.Name)

		if header.Typeflag == tar.TypeReg && !strings.Contains(header.Name, "._") && codeExtensions[ext] {
			content, err := io.ReadAll(tr)
			if err != nil {
				log.Info().Msgf("failed to read: %v", err)
				continue
			}

			AllCode = append(AllCode, "// File: "+header.Name+"\n"+string(content))
		}
	}

	return AllCode, nil
}

func getPackageCode(tarballURL string) (*tar.Reader, error) {
	if tarballURL == "" {
		log.Error().Msg("tarball URL is empty")
		return nil, fmt.Errorf("tarball URL is empty")
	}

	// Download the tarball
	tarResp, err := http.Get(tarballURL) //nolint:all
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
	return tr, nil
}
