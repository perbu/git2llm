package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGit2LLMIntegration tests end-to-end functionality with real filesystem
func TestGit2LLMIntegration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test files and directories
	testFiles := map[string]string{
		"main.go":          "package main\n\nfunc main() {\n\tfmt.Println(\"Hello World\")\n}",
		"README.md":        "# Test Project\n\nThis is a test project for git2llm.",
		"config.json":      "{\n\t\"name\": \"test\",\n\t\"version\": \"1.0.0\"\n}",
		"src/utils.go":     "package main\n\nfunc helper() string {\n\treturn \"helper\"\n}",
		"src/main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) {\n\t// test\n}",
		".gitignore":       "*.log\nnode_modules/\n",
		".env":             "SECRET_KEY=supersecret123",
		"vendor/lib.go":    "package vendor\n\n// vendor code",
		"data.log":         "2023-01-01 INFO: Starting application",
	}

	// Create the directory structure and files
	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		dir := filepath.Dir(fullPath)

		// Create directory if it doesn't exist
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		// Write file content
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fullPath, err)
		}
	}

	// Test 1: Basic scan without filters
	t.Run("basic_scan", func(t *testing.T) {
		git2llm, err := NewGit2LLM(tempDir, nil, nil, nil, false, false, false, nil, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()

		// Should include main files
		if !strings.Contains(result, "main.go") {
			t.Error("Expected main.go to be included")
		}
		if !strings.Contains(result, "README.md") {
			t.Error("Expected README.md to be included")
		}

		// Should exclude dotfiles by default
		if strings.Contains(result, ".gitignore") {
			t.Error("Expected .gitignore to be excluded")
		}
		if strings.Contains(result, ".env") {
			t.Error("Expected .env to be excluded")
		}

		// Should include directory structure
		if !strings.Contains(result, "Directory Structure:") {
			t.Error("Expected directory structure section")
		}
		if !strings.Contains(result, "File Contents:") {
			t.Error("Expected file contents section")
		}
	})

	// Test 2: Scan with file type filtering
	t.Run("go_files_only", func(t *testing.T) {
		git2llm, err := NewGit2LLM(tempDir, []string{".go"}, nil, nil, false, false, false, nil, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()

		// Should include Go files
		if !strings.Contains(result, "main.go") {
			t.Error("Expected main.go to be included")
		}
		if !strings.Contains(result, "src/utils.go") {
			t.Error("Expected src/utils.go to be included")
		}

		// Should exclude non-Go files
		if strings.Contains(result, "README.md") {
			t.Error("Did not expect README.md to be included")
		}
		if strings.Contains(result, "config.json") {
			t.Error("Did not expect config.json to be included")
		}
	})

	// Test 3: Exclude tests
	t.Run("exclude_tests", func(t *testing.T) {
		git2llm, err := NewGit2LLM(tempDir, []string{".go"}, nil, nil, false, true, false, nil, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()

		// Should include regular Go files
		if !strings.Contains(result, "main.go") {
			t.Error("Expected main.go to be included")
		}
		if !strings.Contains(result, "src/utils.go") {
			t.Error("Expected src/utils.go to be included")
		}

		// Should exclude test files
		if strings.Contains(result, "main_test.go") {
			t.Error("Did not expect main_test.go to be included when excluding tests")
		}
	})

	// Test 4: Custom exclusion patterns
	t.Run("custom_exclusions", func(t *testing.T) {
		excludePatterns := []string{"vendor", "*.log"}
		git2llm, err := NewGit2LLM(tempDir, nil, nil, nil, false, false, false, excludePatterns, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()

		// Should exclude vendor directory
		if strings.Contains(result, "vendor/lib.go") {
			t.Error("Did not expect vendor/lib.go to be included")
		}

		// Should exclude log files
		if strings.Contains(result, "data.log") {
			t.Error("Did not expect data.log to be included")
		}

		// Should still include regular files
		if !strings.Contains(result, "main.go") {
			t.Error("Expected main.go to be included")
		}
	})

	// Test 5: Non-recursive mode
	t.Run("non_recursive", func(t *testing.T) {
		git2llm, err := NewGit2LLM(tempDir, nil, nil, nil, false, false, false, nil, "", true)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()

		// Should include files in root directory
		if !strings.Contains(result, "main.go") {
			t.Error("Expected main.go to be included")
		}
		if !strings.Contains(result, "README.md") {
			t.Error("Expected README.md to be included")
		}

		// Should NOT include files in subdirectories
		if strings.Contains(result, "src/utils.go") {
			t.Error("Did not expect src/utils.go to be included in non-recursive mode")
		}
		if strings.Contains(result, "vendor/lib.go") {
			t.Error("Did not expect vendor/lib.go to be included in non-recursive mode")
		}
	})
}

// TestGit2LLMWithLLMIgnoreFile tests .llmignore functionality
func TestGit2LLMWithLLMIgnoreFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"main.go":        "package main",
		"secret.key":     "secret content",
		"public.txt":     "public content",
		"temp/cache.tmp": "cache data",
		"logs/app.log":   "log data",
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fullPath, err)
		}
	}

	// Create .llmignore file
	llmIgnoreContent := `# Ignore secret files
*.key
# Ignore temp directories
temp/
# Ignore log files
*.log
logs/
`
	llmIgnorePath := filepath.Join(tempDir, ".llmignore")
	if err := os.WriteFile(llmIgnorePath, []byte(llmIgnoreContent), 0644); err != nil {
		t.Fatalf("Failed to write .llmignore file: %v", err)
	}

	// Test scanning with .llmignore
	git2llm, err := NewGit2LLM(tempDir, nil, nil, nil, false, false, false, nil, "", false)
	if err != nil {
		t.Fatalf("NewGit2LLM failed: %v", err)
	}

	var output strings.Builder
	git2llm.outputWriter = &output

	err = git2llm.ScanRepository()
	if err != nil {
		t.Fatalf("ScanRepository failed: %v", err)
	}

	result := output.String()

	// Should include allowed files
	if !strings.Contains(result, "main.go") {
		t.Error("Expected main.go to be included")
	}
	if !strings.Contains(result, "public.txt") {
		t.Error("Expected public.txt to be included")
	}

	// Should exclude files matching patterns in .llmignore
	if strings.Contains(result, "secret.key") {
		t.Errorf("Did not expect secret.key to be included. Result:\n%s", result)
	}
	if strings.Contains(result, "cache.tmp") {
		t.Errorf("Did not expect temp/cache.tmp to be included. Result:\n%s", result)
	}
	if strings.Contains(result, "app.log") {
		t.Errorf("Did not expect logs/app.log to be included. Result:\n%s", result)
	}
}

// TestGit2LLMErrorHandling tests various error conditions
func TestGit2LLMErrorHandling(t *testing.T) {
	// Test with non-existent directory
	t.Run("non_existent_directory", func(t *testing.T) {
		git2llm, err := NewGit2LLM("/non/existent/path", nil, nil, nil, false, false, false, nil, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		// This should handle the error gracefully
		err = git2llm.ScanRepository()
		if err == nil {
			t.Error("Expected an error when scanning non-existent directory")
		}
	})

	// Test with unreadable files (permissions)
	t.Run("unreadable_file", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("Skipping permission test when running as root")
		}

		tempDir := t.TempDir()

		// Create a file and make it unreadable
		testFile := filepath.Join(tempDir, "unreadable.txt")
		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Remove read permissions
		if err := os.Chmod(testFile, 0000); err != nil {
			t.Fatalf("Failed to change file permissions: %v", err)
		}

		// Restore permissions after test
		defer func() {
			os.Chmod(testFile, 0644)
		}()

		git2llm, err := NewGit2LLM(tempDir, nil, nil, nil, false, false, false, nil, "", false)
		if err != nil {
			t.Fatalf("NewGit2LLM failed: %v", err)
		}

		var output strings.Builder
		git2llm.outputWriter = &output

		// Should handle unreadable files gracefully
		err = git2llm.ScanRepository()
		if err != nil {
			t.Fatalf("ScanRepository failed: %v", err)
		}

		result := output.String()
		// Should still generate output, just skip the unreadable file
		if !strings.Contains(result, "Directory Structure:") {
			t.Error("Expected directory structure to be generated despite unreadable file")
		}
	})
}
