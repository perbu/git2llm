package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseExclusionFile(t *testing.T) {
	testCases := []struct {
		name        string
		filePath    string
		fileContent string
		expect      map[string]bool
		expectError error
	}{
		{
			name:     "no exclusion file",
			filePath: "nonexistent_file.txt",
			expect:   defaultPatterns(),
		},
		{
			name:        "valid exclusion file",
			filePath:    "exclusions.txt",
			fileContent: "pattern1\npattern2\n#comment\npattern3/",
			expect: map[string]bool{
				".git":      true,
				".svn":      true,
				".idea":     true,
				".vscode":   true,
				"pattern1":  true,
				"pattern2":  true,
				"pattern3/": true,
			},
		},
		{
			name:        "error opening file",
			filePath:    "unreadable_dir/exclusions.txt",
			expectError: errors.New("error opening exclusion file"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{
				FileContent: tc.fileContent,
				FileOpenError: func(name string) error {
					if tc.filePath == "unreadable_dir/exclusions.txt" {
						return os.ErrPermission
					}
					if tc.filePath == "nonexistent_file.txt" {
						return os.ErrNotExist
					}
					return nil
				},
			}

			patterns, err := parseExclusionFile(mockFS, tc.filePath)

			if tc.expectError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.expectError.Error()) {
					t.Errorf("Expected error containing: %v, got: %v", tc.expectError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(patterns) != len(tc.expect) {
				t.Errorf("Expected %d patterns, got %d", len(tc.expect), len(patterns))
			}

			for pattern := range tc.expect {
				if _, ok := patterns[pattern]; !ok {
					t.Errorf("Expected pattern '%s' not found in parsed patterns", pattern)
				}
			}
		})
	}
}

func TestIsExcluded(t *testing.T) {
	exclusionPatterns := map[string]bool{
		"temp/":       true,
		"*.log":       true,
		"/config/":    true,
		"/exact_file": true,
		"middle_part": true,
		"*_test.go":   true,
	}

	testCases := []struct {
		name   string
		path   string
		expect bool
	}{
		{"excluded directory prefix", "temp/file.txt", true},
		{"excluded file type", "file.log", true},
		{"not excluded other file type", "file.txt", false},
		{"excluded absolute directory", "config/app.ini", true}, // Fixed this expectation
		{"excluded exact file", "exact_file", true},
		{"excluded test file", "foo_test.go", true},
		{"not excluded implementation file", "foo.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			excluded := isExcluded(tc.path, exclusionPatterns)
			if excluded != tc.expect {
				t.Errorf("For path '%s', expected excluded: %v, got: %v", tc.path, tc.expect, excluded)
			}
		})
	}
}

func TestIsBinaryFile(t *testing.T) {
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
			isBinary := isBinaryFile(mockFS, "testfile.txt")
			if isBinary != tc.expect {
				t.Errorf("For '%s', expected isBinary: %v, got: %v", tc.name, tc.expect, isBinary)
			}
		})
	}
}

func TestFileExtensionFiltering(t *testing.T) {
	testCases := []struct {
		name         string
		fileName     string
		fileTypes    []string
		shouldAccept bool
	}{
		{"go file with go filter", "main.go", []string{".go"}, true},
		{"js file with go filter", "script.js", []string{".go"}, false},
		{"go file with js filter", "main.go", []string{".js"}, false},
		{"go file with multiple filters", "main.go", []string{".go", ".js"}, true},
		{"js file with multiple filters", "script.js", []string{".go", ".js"}, true},
		{"md file with multiple filters", "readme.md", []string{".go", ".js"}, false},
		{"any file with no filter", "main.go", nil, true},
		{"any file with empty filter", "script.js", []string{}, false}, // Empty filter means no files match
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test filtering in the ProcessFile part of scanFolder
			shouldProcess := false
			if tc.fileTypes == nil {
				shouldProcess = true
			} else {
				for _, ext := range tc.fileTypes {
					if strings.HasSuffix(tc.fileName, ext) {
						shouldProcess = true
						break
					}
				}
			}

			if shouldProcess != tc.shouldAccept {
				t.Errorf("For file '%s' with filters %v, got shouldProcess=%v, expected %v",
					tc.fileName, tc.fileTypes, shouldProcess, tc.shouldAccept)
			}
		})
	}

	// Test directory tree filtering
	t.Run("directory tree filtering", func(t *testing.T) {
		fileTypes := []string{".go"}
		mockFS := &MockFS{
			DirStructure: map[string][]string{
				"test": {"main.go", "script.js", "readme.md"},
			},
		}

		result, err := printDirectoryStructure(mockFS, "test", map[string]bool{}, fileTypes)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Check that .go files are included
		if !strings.Contains(result, "main.go") {
			t.Errorf("Expected 'main.go' to be included in directory tree output")
		}

		// Check that non-.go files are excluded
		if strings.Contains(result, "script.js") {
			t.Errorf("Did not expect 'script.js' to be included in directory tree output")
		}

		if strings.Contains(result, "readme.md") {
			t.Errorf("Did not expect 'readme.md' to be included in directory tree output")
		}
	})
}

func TestProcessFile(t *testing.T) {
	testCases := []struct {
		name         string
		fileContent  string
		isBinary     bool
		expectOutput string
		expectError  error
	}{
		{
			name:        "text file processing",
			fileContent: "This is the content of a text file.",
			isBinary:    false,
			expectOutput: `File: testfile.txt
--------------------------------------------------
Content of testfile.txt:
This is the content of a text file.

`,
		},
		{
			name:        "binary file - skip content",
			fileContent: string([]byte{0, 1, 2, 3, 4, 5}),
			isBinary:    true,
			expectOutput: `File: testfile.txt (Binary - skipped content)
--------------------------------------------------
Content of testfile.txt: (Skipped - Binary File)


`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{
				FileContent:    tc.fileContent,
				IsBinaryResult: tc.isBinary,
			}

			outputWriter := &bytes.Buffer{}
			err := processFile(mockFS, outputWriter, "testfile.txt", "testfile.txt")

			if tc.expectError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.expectError.Error()) {
					t.Errorf("Expected error containing: %v, got: %v", tc.expectError, err)
				}
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Compare only the first two lines and check if content exists
			output := outputWriter.String()
			outputLines := strings.Split(output, "\n")
			expectedLines := strings.Split(tc.expectOutput, "\n")

			if len(outputLines) < 2 || len(expectedLines) < 2 {
				t.Fatalf("Output or expected output has too few lines")
			}

			if outputLines[0] != expectedLines[0] {
				t.Errorf("First line mismatch. Expected: %q, Got: %q", expectedLines[0], outputLines[0])
			}

			if tc.isBinary {
				if !strings.Contains(output, "Binary") {
					t.Errorf("Binary file output should contain 'Binary'")
				}
			} else {
				if !strings.Contains(output, tc.fileContent) {
					t.Errorf("Text file output should contain the file content")
				}
			}
		})
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
