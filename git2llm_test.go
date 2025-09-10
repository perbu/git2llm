package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStringSliceFlag(t *testing.T) {
	var flag stringSliceFlag

	// Test initial state
	if flag.String() != "" {
		t.Errorf("Expected empty string, got: %s", flag.String())
	}

	// Test adding a single value
	err := flag.Set("vendor")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if flag.String() != "vendor" {
		t.Errorf("Expected 'vendor', got: %s", flag.String())
	}

	// Test adding multiple values
	err = flag.Set("node_modules")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if flag.String() != "vendor, node_modules" {
		t.Errorf("Expected 'vendor, node_modules', got: %s", flag.String())
	}

	// Verify the slice contains all values
	if len(flag) != 2 {
		t.Errorf("Expected 2 items, got: %d", len(flag))
	}
	if flag[0] != "vendor" || flag[1] != "node_modules" {
		t.Errorf("Expected ['vendor', 'node_modules'], got: %v", flag)
	}
}

// MockFS for testing
type MockFS struct {
	FileContent    string
	FileContentMap map[string]string
	DirStructure   map[string][]string
	IsBinaryResult bool
	ReadFileError  error
	ReadDirError   error
	FileOpenError  func(name string) error
}

type MockFile struct {
	content string
	closed  bool
}

func (m *MockFile) Read(p []byte) (n int, err error) {
	if m.content == "" {
		return 0, io.EOF
	}
	n = copy(p, m.content)
	m.content = m.content[n:]
	return n, nil
}

func (m *MockFile) Close() error {
	m.closed = true
	return nil
}

func (m *MockFS) Open(name string) (File, error) {
	if m.FileOpenError != nil {
		if err := m.FileOpenError(name); err != nil {
			return nil, err
		}
	}

	// Use FileContentMap if available
	if m.FileContentMap != nil {
		if content, ok := m.FileContentMap[name]; ok {
			return &MockFile{content: content}, nil
		}
	}

	return &MockFile{content: m.FileContent}, nil
}

func (m *MockFS) ReadDir(name string) ([]os.DirEntry, error) {
	if m.ReadDirError != nil {
		return nil, m.ReadDirError
	}
	entries := []os.DirEntry{}
	if files, ok := m.DirStructure[name]; ok {
		for _, file := range files {
			isDir := strings.HasSuffix(file, "/") || isDirInStructure(filepath.Join(name, file), m.DirStructure)
			entries = append(entries, mockDirEntry{name: file, isDir: isDir})
		}
	}
	return entries, nil
}

func isDirInStructure(path string, structure map[string][]string) bool {
	_, exists := structure[path]
	return exists
}

func (m *MockFS) ReadFile(name string) ([]byte, error) {
	if m.ReadFileError != nil {
		return nil, m.ReadFileError
	}

	// Use FileContentMap if available
	if m.FileContentMap != nil {
		if content, ok := m.FileContentMap[name]; ok {
			return []byte(content), nil
		}
	}

	return []byte(m.FileContent), nil
}

func (m *MockFS) Stat(name string) (os.FileInfo, error) {
	return mockFileInfo{}, nil
}

func (m *MockFS) Lstat(name string) (os.FileInfo, error) {
	return mockFileInfo{}, nil
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string { return m.name }
func (m mockDirEntry) IsDir() bool  { return m.isDir }
func (m mockDirEntry) Type() os.FileMode {
	if m.isDir {
		return os.ModeDir
	}
	return 0
}
func (m mockDirEntry) Info() (os.FileInfo, error) {
	return mockFileInfo{name: m.name, isDir: m.isDir}, nil
}

type mockFileInfo struct {
	name  string
	isDir bool
}

func (m mockFileInfo) Name() string { return m.name }
func (m mockFileInfo) Size() int64  { return 0 }
func (m mockFileInfo) Mode() os.FileMode {
	if m.isDir {
		return os.ModeDir
	}
	return 0
}
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func TestNewGit2LLM(t *testing.T) {
	testCases := []struct {
		name            string
		startPath       string
		fileTypes       []string
		verbose         bool
		excludeTests    bool
		excludePatterns []string
	}{
		{
			name:            "basic initialization",
			startPath:       "/test/path",
			fileTypes:       []string{".go", ".js"},
			verbose:         true,
			excludeTests:    false,
			excludePatterns: []string{"vendor", "node_modules"},
		},
		{
			name:            "exclude tests",
			startPath:       "/test/path",
			fileTypes:       nil,
			verbose:         false,
			excludeTests:    true,
			excludePatterns: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{
				FileContent: "pattern1\npattern2\n",
			}

			git2llm, err := NewGit2LLM(tc.startPath, tc.fileTypes, mockFS, nil, tc.verbose, tc.excludeTests, false, tc.excludePatterns, "", false)
			if err != nil {
				t.Fatalf("NewGit2LLM failed: %v", err)
			}

			if git2llm.startPath != tc.startPath {
				t.Errorf("Expected startPath %s, got %s", tc.startPath, git2llm.startPath)
			}

			if git2llm.verbose != tc.verbose {
				t.Errorf("Expected verbose %v, got %v", tc.verbose, git2llm.verbose)
			}

			if git2llm.excludeTests != tc.excludeTests {
				t.Errorf("Expected excludeTests %v, got %v", tc.excludeTests, git2llm.excludeTests)
			}

			// Check file types
			if len(tc.fileTypes) != len(git2llm.fileTypes) {
				t.Errorf("Expected %d file types, got %d", len(tc.fileTypes), len(git2llm.fileTypes))
			}

			// Check custom exclusion patterns were added
			for _, pattern := range tc.excludePatterns {
				if !git2llm.exclusionPatterns[pattern] {
					t.Errorf("Expected exclusion pattern %s to be present", pattern)
				}
			}

			// Check default patterns are present
			if !git2llm.exclusionPatterns[".git"] {
				t.Errorf("Expected default .git exclusion pattern to be present")
			}
		})
	}
}

func TestGit2LLMIsExcluded(t *testing.T) {
	git2llm := &Git2LLM{
		exclusionPatterns: map[string]bool{
			"temp/":       true,
			"*.log":       true,
			"/config/":    true,
			"/exact_file": true,
			"middle_part": true,
			"*_test.go":   true,
		},
	}

	testCases := []struct {
		name   string
		path   string
		expect bool
	}{
		{"excluded directory prefix", "temp/file.txt", true},
		{"excluded file type", "file.log", true},
		{"not excluded other file type", "file.txt", false},
		{"excluded absolute directory", "config/app.ini", true},
		{"excluded exact file", "exact_file", true},
		{"excluded test file", "foo_test.go", true},
		{"not excluded implementation file", "foo.go", false},
		{"excluded dotfile", ".gitignore", true},
		{"excluded dotfile with extension", ".env.local", true},
		{"excluded dotfolder", ".config/app.ini", true},
		{"excluded nested dotfile", "src/.DS_Store", true},
		{"excluded dotfolder with regular file", ".vscode/settings.json", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			excluded := git2llm.isExcluded(tc.path)
			if excluded != tc.expect {
				t.Errorf("For path '%s', expected excluded: %v, got: %v", tc.path, tc.expect, excluded)
			}
		})
	}
}

func TestGit2LLMIsForbiddenFile(t *testing.T) {
	testCases := []struct {
		name           string
		fileContent    string
		expectedReason string
	}{
		{
			name:           "text file",
			fileContent:    "This is a text file.",
			expectedReason: "",
		},
		{
			name:           "binary file with null byte",
			fileContent:    string([]byte{0, 1, 2, 3, 4, 5}),
			expectedReason: "binary",
		},
		{
			name:           "private key file",
			fileContent:    "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC...\n-----END PRIVATE KEY-----",
			expectedReason: "private key",
		},
		{
			name:           "RSA private key",
			fileContent:    "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----",
			expectedReason: "private key",
		},
		{
			name:           "large text file",
			fileContent:    strings.Repeat("Hello World\n", 1000),
			expectedReason: "",
		},
		{
			name:           "empty file",
			fileContent:    "",
			expectedReason: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{FileContent: tc.fileContent}
			git2llm := &Git2LLM{fs: mockFS}

			reason := git2llm.isForbiddenFile("testfile.txt")
			if reason != tc.expectedReason {
				t.Errorf("For '%s', expected reason: '%s', got: '%s'", tc.name, tc.expectedReason, reason)
			}
		})
	}
}

func TestGit2LLMLoadExclusionPatterns(t *testing.T) {
	testCases := []struct {
		name            string
		fileContent     string
		expectedPattern string
		shouldExist     bool
	}{
		{
			name:            "basic pattern loading",
			fileContent:     "vendor/\nnode_modules/\n# comment\n\n",
			expectedPattern: "vendor/",
			shouldExist:     true,
		},
		{
			name:            "ignore comments and empty lines",
			fileContent:     "# This is a comment\nvalid_pattern\n\n# Another comment",
			expectedPattern: "valid_pattern",
			shouldExist:     true,
		},
		{
			name:            "default patterns always present",
			fileContent:     "",
			expectedPattern: ".git",
			shouldExist:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{FileContent: tc.fileContent}
			git2llm := &Git2LLM{fs: mockFS, exclusionPatterns: make(map[string]bool)}

			err := git2llm.loadExclusionPatterns(".llmignore")
			if err != nil {
				t.Fatalf("loadExclusionPatterns failed: %v", err)
			}

			if git2llm.exclusionPatterns[tc.expectedPattern] != tc.shouldExist {
				t.Errorf("Pattern '%s' existence: expected %v, got %v", tc.expectedPattern, tc.shouldExist, git2llm.exclusionPatterns[tc.expectedPattern])
			}
		})
	}
}

func TestGit2LLMDirectoryStructureGeneration(t *testing.T) {
	testCases := []struct {
		name         string
		dirStructure map[string][]string
		startPath    string
		expected     []string // strings that should be present in output
		notExpected  []string // strings that should NOT be present in output
	}{
		{
			name: "basic directory structure",
			dirStructure: map[string][]string{
				".":    {"file1.go", "dir1", "file2.txt"},
				"dir1": {"nested.go"},
			},
			startPath:   ".",
			expected:    []string{"file1.go", "file2.txt", "dir1/", "nested.go"},
			notExpected: []string{},
		},
		{
			name: "exclude dotfiles",
			dirStructure: map[string][]string{
				".": {"file1.go", ".gitignore", ".hidden", "normal.txt"},
			},
			startPath:   ".",
			expected:    []string{"file1.go", "normal.txt"},
			notExpected: []string{".gitignore", ".hidden"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{DirStructure: tc.dirStructure}
			git2llm := &Git2LLM{
				fs:                mockFS,
				startPath:         tc.startPath,
				exclusionPatterns: defaultPatterns(),
			}

			result, err := git2llm.generateDirectoryStructureString()
			if err != nil {
				t.Fatalf("generateDirectoryStructureString failed: %v", err)
			}

			for _, expected := range tc.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected '%s' to be in output, but it wasn't. Output:\n%s", expected, result)
				}
			}

			for _, notExpected := range tc.notExpected {
				if strings.Contains(result, notExpected) {
					t.Errorf("Did not expect '%s' to be in output, but it was. Output:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestGit2LLMNonRecursiveMode(t *testing.T) {
	mockFS := &MockFS{
		DirStructure: map[string][]string{
			".":           {"file1.go", "subdir", "file2.txt"},
			"subdir":      {"nested.go", "deep.txt"},
			"subdir/sub2": {"verydeep.go"},
		},
		FileContentMap: map[string]string{
			"file1.go":  "package main\nfunc main() {}",
			"file2.txt": "hello world",
		},
	}

	// Test recursive mode (default)
	git2llm, err := NewGit2LLM(".", nil, mockFS, nil, false, false, false, nil, "", false)
	if err != nil {
		t.Fatalf("NewGit2LLM failed: %v", err)
	}

	var output strings.Builder
	git2llm.outputWriter = &output
	err = git2llm.ScanRepository()
	if err != nil {
		t.Fatalf("ScanRepository failed: %v", err)
	}

	recursiveOutput := output.String()

	// Test non-recursive mode
	git2llm2, err := NewGit2LLM(".", nil, mockFS, nil, false, false, false, nil, "", true) // noRecurse = true
	if err != nil {
		t.Fatalf("NewGit2LLM failed: %v", err)
	}

	var output2 strings.Builder
	git2llm2.outputWriter = &output2
	err = git2llm2.ScanRepository()
	if err != nil {
		t.Fatalf("ScanRepository failed: %v", err)
	}

	nonRecursiveOutput := output2.String()

	// In non-recursive mode, should only see files in root directory
	if !strings.Contains(nonRecursiveOutput, "file1.go") {
		t.Error("Expected file1.go in non-recursive output")
	}
	if !strings.Contains(nonRecursiveOutput, "file2.txt") {
		t.Error("Expected file2.txt in non-recursive output")
	}

	// Should see subdirectory files in recursive mode but not in non-recursive
	if strings.Contains(nonRecursiveOutput, "nested.go") {
		t.Error("Did not expect nested.go in non-recursive output")
	}
	if !strings.Contains(recursiveOutput, "nested.go") {
		t.Error("Expected nested.go in recursive output")
	}
}

func TestGit2LLMFileTypeFiltering(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"script.py":   "print('hello')\n",
		"config.json": "{}\n",
		"readme.txt":  "This is a readme\n",
	}

	for fileName, content := range testFiles {
		filePath := filepath.Join(tempDir, fileName)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", fileName, err)
		}
	}

	// Test filtering for only Go files
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
	if !strings.Contains(result, "package main") {
		t.Error("Expected main.go content to be included")
	}

	// Should not include other file types
	if strings.Contains(result, "script.py") {
		t.Error("Did not expect script.py to be included")
	}
	if strings.Contains(result, "config.json") {
		t.Error("Did not expect config.json to be included")
	}
}
