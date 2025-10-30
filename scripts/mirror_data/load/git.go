package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

func ScanRepo(ctx context.Context, client *genai.Client, model string, mcp Server) (*PaloMeta, error) {
	if mcp.Repository.URL == "" {
		return nil, fmt.Errorf("repository URL is empty")
	}

	AllCode, err := ReadAllCodeFilesFromRepo(mcp.Repository.URL, mcp.Version)
	if err != nil {
		log.Warn().Msgf("Failed to retrieve code err: %v", err)
	}

	Palo, err := Dispatch(ctx, AllCode, client, model)
	if err != nil {
		log.Error().Msgf("Error dispatching library: %v", err)
		return nil, err
	}

	return Palo, nil
}

// ReadAllCodeFilesFromRepo clones/reads a git repository and returns all code file contents
func ReadAllCodeFilesFromRepo(repoURL string, version string) ([]string, error) {
	if repoURL == "" {
		return nil, fmt.Errorf("repository URL is empty")
	}

	// Create temporary directory for cloning
	tempDir, err := os.MkdirTemp("", "git-repo-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	repo, err := cloneRepositoryWithFallback(repoURL, version, tempDir)
	if err != nil {
		return nil, err
	}

	// Get the HEAD commit (or the checked out version)
	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree: %w", err)
	}

	var codeContents []string

	// Walk through all files in the repository
	err = tree.Files().ForEach(func(file *object.File) error {
		// Skip if path should be skipped
		if shouldSkipPath(file.Name) {
			return nil
		}

		// Skip if it's not a code file
		if !isCodeFile(file.Name) {
			return nil
		}

		// Skip if it's a test file
		if isTestFile(file.Name) {
			return nil
		}

		// Skip if it's a non-code file (config, docs, etc.)
		if shouldSkipFile(file.Name) {
			return nil
		}

		// Skip large files (> 1MB)
		if file.Size > 1024*1024 {
			return nil
		}

		// Read file content
		content, err := file.Contents()
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file.Name, err)
		}

		// Add file path as comment and content
		fileContent := fmt.Sprintf("// File: %s\n%s", file.Name, content)
		codeContents = append(codeContents, fileContent)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk repository files: %w", err)
	}

	return codeContents, nil
}

// Extract clone strategy creation
func createCloneOptions(repoURL string) *git.CloneOptions {
	return &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
		Depth:    1,
	}
}

func setVersionReference(options *git.CloneOptions, version, refType string) {
	if version != "" && version != "latest" {
		options.ReferenceName = plumbing.ReferenceName(refType + version)
		options.SingleBranch = true
	}
}

func setFullCloneOptions(options *git.CloneOptions) {
	options.ReferenceName = ""
	options.SingleBranch = false
	options.Depth = 0
}

// Extract individual clone attempts
func tryCloneAsTag(tempDir, repoURL, version string) (*git.Repository, error) {
	options := createCloneOptions(repoURL)
	setVersionReference(options, version, "refs/tags/")
	return git.PlainClone(tempDir, false, options)
}

func tryCloneAsBranch(tempDir, repoURL, version string) (*git.Repository, error) {
	options := createCloneOptions(repoURL)
	setVersionReference(options, version, "refs/heads/")
	return git.PlainClone(tempDir, false, options)
}

func tryCloneAndCheckout(tempDir, repoURL, version string) (*git.Repository, error) {
	options := createCloneOptions(repoURL)
	setFullCloneOptions(options)

	repo, err := git.PlainClone(tempDir, false, options)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	err = checkoutVersion(repo, version)
	if err != nil {
		return nil, fmt.Errorf("failed to checkout version %s: %w", version, err)
	}

	return repo, nil
}

func cloneRepositoryWithFallback(repoURL, version, tempDir string) (*git.Repository, error) {
	// Handle simple case first
	if version == "" || version == "latest" {
		options := createCloneOptions(repoURL)
		return git.PlainClone(tempDir, false, options)
	}

	// Try strategies in order for versioned clones
	cloneStrategies := []func() (*git.Repository, error){
		func() (*git.Repository, error) { return tryCloneAsTag(tempDir, repoURL, version) },
		func() (*git.Repository, error) { return tryCloneAsBranch(tempDir, repoURL, version) },
		func() (*git.Repository, error) { return tryCloneAndCheckout(tempDir, repoURL, version) },
	}

	for _, strategy := range cloneStrategies {
		if repo, err := strategy(); err == nil {
			return repo, nil
		}
	}

	return nil, fmt.Errorf("failed to clone repository with version %s", version)
}

// checkoutVersion attempts to check out a specific version (tag, branch, or commit)
func checkoutVersion(repo *git.Repository, version string) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get w orktree: %w", err)
	}

	// Try to resolve as tag first
	tagRef, err := repo.Tag(version)
	if err == nil {
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: tagRef.Hash(),
		})
		if err == nil {
			return nil
		}
	}

	// Try to resolve as branch
	branchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+version), true)
	if err == nil {
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: branchRef.Hash(),
		})
		if err == nil {
			return nil
		}
	}

	// Try to resolve as remote branch
	remoteBranchRef, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+version), true)
	if err == nil {
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: remoteBranchRef.Hash(),
		})
		if err == nil {
			return nil
		}
	}

	// Try to resolve as commit hash
	if len(version) >= 7 { // Minimum length for a commit hash
		hash := plumbing.NewHash(version)
		if !hash.IsZero() {
			err = worktree.Checkout(&git.CheckoutOptions{
				Hash: hash,
			})
			if err == nil {
				return nil
			}
		}
	}

	return fmt.Errorf("version %s not found as tag, branch, or commit", version)
}

// isCodeFile determines if a file is a code file based on extension
func isCodeFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))

	codeExtensions := map[string]bool{
		".py":    true, // Python
		".js":    true, // JavaScript
		".ts":    true, // TypeScript
		".go":    true, // Go
		".java":  true, // Java
		".c":     true, // C
		".cpp":   true, // C++
		".h":     true, // Header files
		".hpp":   true, // C++ headers
		".cs":    true, // C#
		".php":   true, // PHP
		".rb":    true, // Ruby
		".rs":    true, // Rust
		".kt":    true, // Kotlin
		".swift": true, // Swift
		".scala": true, // Scala
		".sh":    true, // Shell scripts
		".ps1":   true, // PowerShell
		".sql":   true, // SQL
		".r":     true, // R
		".m":     true, // Objective-C/MATLAB
		".pl":    true, // Perl
		".lua":   true, // Lua
		".dart":  true, // Dart
		".elm":   true, // Elm
		".ex":    true, // Elixir
		".exs":   true, // Elixir scripts
		".clj":   true, // Clojure
		".hs":    true, // Haskell
		".ml":    true, // OCaml
		".fs":    true, // F#
		".vb":    true, // Visual Basic
	}

	return codeExtensions[ext]
}

// isTestFile determines if a file is a unit test file
func isTestFile(filename string) bool {
	lowerName := strings.ToLower(filename)
	baseName := strings.ToLower(filepath.Base(filename))

	// Common test file patterns
	testPatterns := []string{
		"test_",       // Python: test_example.py
		"_test.",      // Go: example_test.go
		".test.",      // General: example.test.js
		".spec.",      // Spec files: example.spec.js
		"_spec.",      // Ruby: example_spec.rb
		"test.",       // test.py, test.js
		"tests.",      // tests.py
		"_tests.",     // example_tests.py
		".tests.",     // example.tests.js
		"conftest.",   // Python: conftest.py
		"setup_test.", // setup_test.py
		"test_setup.", // test_setup.py
	}

	// Check filename patterns
	for _, pattern := range testPatterns {
		if strings.Contains(baseName, pattern) {
			return true
		}
	}

	// Check if filename starts with Test (Java/C# convention)
	if strings.HasPrefix(baseName, "test") {
		return true
	}

	// Check if filename ends with Test or Tests
	nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if strings.HasSuffix(nameWithoutExt, "test") || strings.HasSuffix(nameWithoutExt, "tests") {
		return true
	}

	// Check path contains test directories
	testDirs := []string{
		"/test/",
		"/tests/",
		"/testing/",
		"/__tests__/",
		"/spec/",
		"/specs/",
		"/unittest/",
		"/unit_tests/",
		"/integration_tests/",
		"/e2e/",
		"/cypress/",
		"/jest/",
		"/mocha/",
		"/karma/",
	}

	for _, testDir := range testDirs {
		if strings.Contains(lowerName, testDir) {
			return true
		}
	}

	return false
}

// shouldSkipPath determines if a path should be skipped
func shouldSkipPath(path string) bool {
	lowerPath := strings.ToLower(path)

	skipDirs := []string{
		".git",
		"node_modules",
		"vendor",
		"target",
		"build",
		"dist",
		"out",
		"bin",
		"obj",
		"__pycache__",
		".pytest_cache",
		".venv",
		"venv",
		".env",
		".idea",
		".vscode",
		".vs",
		"coverage",
		".coverage",
		".nyc_output",
		"htmlcov",
		".tox",
		"__snapshots__",
		".parcel-cache",
		".cache",
		"tmp",
		"temp",
		"logs",
		"log",
		".DS_Store",
		"Thumbs.db",
	}

	for _, skipDir := range skipDirs {
		if strings.Contains(lowerPath, string(os.PathSeparator)+skipDir+string(os.PathSeparator)) ||
			strings.HasSuffix(lowerPath, string(os.PathSeparator)+skipDir) ||
			strings.Contains(lowerPath, "/"+skipDir+"/") ||
			strings.HasSuffix(lowerPath, "/"+skipDir) {
			return true
		}
	}

	return false
}
