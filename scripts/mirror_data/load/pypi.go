package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

func ScanPypiPackages(ctx context.Context, client *genai.Client, model string, mcp Server) (*PaloMeta, error) {

	if len(mcp.Packages) == 0 {
		return nil, fmt.Errorf("no packages found")
	}

	packageName := mcp.Packages[0].Identifier
	version := mcp.Packages[0].Version

	if packageName == "" {
		return nil, fmt.Errorf("package name %s is empty", packageName)
	}

	AllCode, err := DownloadAndExtractWheel(packageName, version)
	if err != nil {
		return nil, err
	}

	Palo, err := Dispatch(ctx, AllCode, client, model)
	if err != nil {
		log.Error().Msgf("Error dispatching library: %v", err)
		return nil, err
	}

	return Palo, nil
}

// DownloadAndExtractWheel downloads a wheel file from PyPI and extracts its contents
func DownloadAndExtractWheel(packageName string, version string) ([]string, error) {
	// Construct PyPI download URL
	wheelURL := fmt.Sprintf("https://pypi.org/pypi/%s/%s/json", packageName, version)

	log.Info().Msgf("Downloading %s", wheelURL)

	// Get package metadata to find wheel download URL
	resp, err := http.Get(wheelURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get package metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download package: %s", resp.Status)
	}

	var pip Pip

	if err := json.NewDecoder(resp.Body).Decode(&pip); err != nil {
		log.Error().Msgf("Failed to decode registry response: %v", err)
		return nil, err
	}

	// Parse JSON response to find wheel file URL
	// You'll need to implement JSON parsing here
	wheelDownloadURL, err := findWheelURL(pip, packageName, version)
	if err != nil {
		return nil, fmt.Errorf("failed to find wheel URL: %w", err)
	}

	// Download the wheel file
	wheelResp, err := http.Get(wheelDownloadURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download wheel: %w", err)
	}
	defer wheelResp.Body.Close()

	// Create temporary file for wheel
	tempDir := os.TempDir()
	wheelPath := filepath.Join(tempDir, fmt.Sprintf("%s-%s.whl", packageName, version))

	wheelFile, err := os.Create(wheelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create wheel file: %w", err)
	}
	defer wheelFile.Close()

	// Copy wheel content to file
	_, err = io.Copy(wheelFile, wheelResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write wheel file: %w", err)
	}

	// Extract Python code directly from wheel
	pythonCode, err := extractPythonCodeFromWheel(wheelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract Python code: %w", err)
	}

	return pythonCode, nil // or however you want to return the data
}

func findWheelURL(pip Pip, packageName, version string) (string, error) {

	for _, url := range pip.Urls {
		if url.PackageType == "bdist_wheel" {
			return url.Url, nil
		}
	}

	// For now, return a placeholder - you'll need to implement JSON parsing
	return "", fmt.Errorf("no wheel URL found for package %s version %s", packageName, version)
}

// extractWheel extracts a wheel file (which is a ZIP archive)
func extractWheel(wheelPath, extractDir string) error {

	//codeExtensions := map[string]bool{
	//	".py": true,
	//}
	reader, err := zip.OpenReader(wheelPath)
	if err != nil {
		return fmt.Errorf("failed to open wheel as zip: %w", err)
	}
	defer reader.Close()

	// Create extraction directory
	err = os.MkdirAll(extractDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create extract directory: %w", err)
	}

	// Extract files
	for _, file := range reader.File {
		path := filepath.Join(extractDir, file.Name)

		// Ensure the file path is within extract directory (security check)
		if !strings.HasPrefix(path, filepath.Clean(extractDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.FileInfo().Mode())
			continue
		}

		// Create file directory if needed
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Extract file
		fileReader, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in wheel: %w", err)
		}

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.FileInfo().Mode())
		if err != nil {
			fileReader.Close()
			return fmt.Errorf("failed to create target file: %w", err)
		}

		_, err = io.Copy(targetFile, fileReader)
		fileReader.Close()
		targetFile.Close()

		if err != nil {
			return fmt.Errorf("failed to copy file content: %w", err)
		}
	}

	return nil
}

func extractPythonCodeFromWheel(wheelPath string) ([]string, error) {
	var pythonCode []string

	// Open wheel file as ZIP archive
	reader, err := zip.OpenReader(wheelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open wheel as zip: %w", err)
	}
	defer reader.Close()

	// Extract Python files directly from ZIP
	for _, file := range reader.File {
		// Check if it's a Python file
		if !strings.HasSuffix(strings.ToLower(file.Name), ".py") {
			continue
		}

		// Skip common non-source files
		if shouldSkipFile(file.Name) {
			continue
		}

		// Open file within ZIP
		fileReader, err := file.Open()
		if err != nil {
			log.Warn().Msgf("Failed to open file %s in wheel: %v", file.Name, err)
			continue
		}

		// Read file content
		content, err := io.ReadAll(fileReader)
		fileReader.Close()

		if err != nil {
			log.Warn().Msgf("Failed to read file %s: %v", file.Name, err)
			continue
		}

		// Add to collection
		pythonCode = append(pythonCode, string(content))
	}

	return pythonCode, nil
}

func shouldSkipFile(fileName string) bool {
	// Skip test files, build files, etc.
	baseName := filepath.Base(fileName)

	return strings.HasPrefix(baseName, "test_") ||
		strings.HasSuffix(baseName, "_test.py") ||
		strings.Contains(fileName, "__pycache__") ||
		strings.Contains(fileName, ".egg-info") ||
		strings.Contains(fileName, "/tests/") ||
		strings.Contains(fileName, "/test/") ||
		baseName == "setup.py" ||
		baseName == "conftest.py"
}
