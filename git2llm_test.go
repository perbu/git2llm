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

			git2llm, err := NewGit2LLM(tc.startPath, tc.fileTypes, mockFS, nil, tc.verbose, tc.excludeTests, tc.excludePatterns)
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

func TestGit2LLMIsBinaryFile(t *testing.T) {
	testCases := []struct {
		name        string
		fileContent string
		expect      bool
	}{
		{
			name:        "text file",
			fileContent: "This is a text file.",
			expect:      false,
		},
		{
			name:        "binary file with null byte",
			fileContent: string([]byte{0, 1, 2, 3, 4, 5}),
			expect:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{FileContent: tc.fileContent}
			git2llm := &Git2LLM{fs: mockFS}

			isBinary := git2llm.isBinaryFile("testfile.txt")
			if isBinary != tc.expect {
				t.Errorf("For '%s', expected isBinary: %v, got: %v", tc.name, tc.expect, isBinary)
			}
		})
	}
}
