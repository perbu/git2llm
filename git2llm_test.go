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
			name:        "empty exclusion file",
			filePath:    "exclusions.txt",
			fileContent: "",
			expect:      defaultPatterns(),
		},
		{
			name:        "valid exclusion file",
			filePath:    "exclusions.txt",
			fileContent: "pattern1\npattern2\n#comment\npattern3/",
			expect: map[string]bool{
				"pattern1":  true,
				"pattern2":  true,
				"pattern3/": true,
			},
		},
		{
			name:        "exclusion file with spaces and empty lines",
			filePath:    "exclusions.txt",
			fileContent: "  pattern1  \n\npattern2\n",
			expect: map[string]bool{
				"pattern1": true,
				"pattern2": true,
			},
		},
		{
			name:        "error opening file",
			filePath:    "unreadable_dir/exclusions.txt",
			expectError: errors.New("error opening exclusion file"), // Expecting an error prefix
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock FS
			mockFS := &MockFS{
				FileContent: tc.fileContent,
				FileOpenError: func(name string) error {
					if strings.Contains(tc.name, "error opening file") {
						return os.ErrPermission // Simulate permission error for "unreadable_dir" case
					}
					if tc.fileContent == "" && tc.name == "exclusions.txt" || tc.fileContent != "" && tc.name == "exclusions.txt" {
						return nil // No error for valid cases if file should exist or empty content case.
					}
					return os.ErrNotExist // Simulate not exist for "nonexistent_file.txt" case and others if needed.
				},
			}
			if tc.filePath == "unreadable_dir/exclusions.txt" {
				mockFS.FileOpenError = func(name string) error {
					return os.ErrPermission // Simulate permission error
				}
			}

			if tc.filePath == "nonexistent_file.txt" {
				mockFS.FileOpenError = func(name string) error {
					return os.ErrNotExist
				}
			}

			if tc.filePath == "exclusions.txt" && tc.fileContent != "" || tc.filePath == "exclusions.txt" && tc.fileContent == "" {
				mockFS.FileOpenError = func(name string) error {
					return nil
				}
			}

			patterns, err := parseExclusionFile(mockFS, tc.filePath)

			if tc.expectError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.expectError.Error()) {
					t.Errorf("Expected error containing: %v, got: %v", tc.expectError, err)
				}
				return // Stop here for error cases
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
		"temp/":        true,
		"*.log":        true,
		"/config/":     true,
		"/exact_file":  true,
		"/dir_prefix/": true,
		"file_suffix/": true,
		"middle_part":  true,
	}

	testCases := []struct {
		name   string
		path   string
		expect bool
	}{
		{"excluded directory prefix", "temp/file.txt", true},
		{"excluded directory exact", "temp/", true},
		{"not excluded directory similar prefix", "temporary/file.txt", false},
		{"excluded file type", "file.log", true},
		{"not excluded other file type", "file.txt", false},
		{"excluded absolute directory", "/config/app.ini", true},
		{"excluded absolute directory root", "/config/", true},
		{"not excluded similar absolute dir", "/configs/app.ini", false},
		{"excluded exact file", "/exact_file", true},
		{"not excluded similar exact file", "/exact_file_diff", false},
		{"excluded dir prefix path", "/dir_prefix/sub/file.txt", true},
		{"excluded dir prefix root", "/dir_prefix/", true},
		{"not excluded similar dir prefix", "/dir_prefixes/sub/file.txt", false},
		{"excluded file suffix dir", "file_suffix/file.txt", true},
		{"excluded file suffix root", "file_suffix/", true},
		{"not excluded similar file suffix", "file_suffix_diff/file.txt", false},
		{"excluded middle part in path", "path/middle_part/file.txt", true},
		{"excluded middle part in filename", "middle_part_file.txt", true},
		{"not excluded similar middle part", "middle_parts/file.txt", false},
		{"not excluded no match", "another/file.txt", false},
		{"path is exactly the pattern without slash", "exact_file", false},       // Should not be excluded if pattern is "/exact_file" and path is "exact_file"
		{"path is directory and matches pattern without slash", "config", false}, // Should not be excluded if pattern is "/config/" and path is "config"
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

func TestPrintDirectoryStructure(t *testing.T) {
	testCases := []struct {
		name              string
		dirStructure      map[string][]string // dir -> []files/dirs
		exclusionPatterns map[string]bool
		expectedOutput    string
		expectError       error
	}{
		{
			name: "simple directory structure without exclusion",
			dirStructure: map[string][]string{
				"root":      {"file1.txt", "dir1"},
				"root/dir1": {"file2.txt"},
			},
			exclusionPatterns: map[string]bool{},
			expectedOutput: `/
├── dir1/
│   └── file2.txt
└── file1.txt
`,
		},
		{
			name: "nested directory structure with exclusion",
			dirStructure: map[string][]string{
				"root":              {"file1.txt", "dir1", "excluded_dir"},
				"root/dir1":         {"file2.txt", "dir2"},
				"root/dir1/dir2":    {"file3.txt"},
				"root/excluded_dir": {"excluded.txt"},
			},
			exclusionPatterns: map[string]bool{"excluded_dir/": true},
			expectedOutput: `/
├── dir1/
│   └── dir2/
│       └── file3.txt
└── file1.txt
`,
		},
		{
			name: "directory with no files or subdirectories",
			dirStructure: map[string][]string{
				"root": {},
			},
			exclusionPatterns: map[string]bool{},
			expectedOutput: `/
`,
		},
		{
			name:              "empty directory structure",
			dirStructure:      map[string][]string{},
			exclusionPatterns: map[string]bool{},
			expectedOutput: `/
`, // Should still print root directory
		},
		{
			name: "error reading directory",
			dirStructure: map[string][]string{
				"root": {"dir1"},
			},
			expectError: errors.New("error reading directory"),
		},
		{
			name: "complex directory structure with sorting",
			dirStructure: map[string][]string{
				"root":              {"fileC.txt", "DirB", "FileA.txt", "dirA"},
				"root/dirA":         {"fileD.txt"},
				"root/DirB":         {"fileE.txt", "subdirC", "SubdirB", "subdirA"},
				"root/DirB/subdirA": {"fileF.txt"},
				"root/DirB/SubdirB": {"fileG.txt"},
				"root/DirB/subdirC": {"fileH.txt"},
			},
			exclusionPatterns: map[string]bool{},
			expectedOutput: `/
├── DirB/
│   ├── SubdirB/
│   │   └── fileG.txt
│   ├── subdirA/
│   │   └── fileF.txt
│   └── subdirC/
│       └── fileH.txt
├── dirA/
│   └── fileD.txt
├── FileA.txt
└── fileC.txt
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{DirStructure: tc.dirStructure}
			if tc.expectError != nil {
				mockFS.ReadDirError = errors.New("read directory error for test") // Simulate read dir error
			}

			output, err := printDirectoryStructure(mockFS, "root", tc.exclusionPatterns)

			if tc.expectError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.expectError.Error()) {
					t.Errorf("Expected error containing: %v, got: %v", tc.expectError, err)
				}
				return // Stop here for error cases
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if output != tc.expectedOutput {
				t.Errorf("Output mismatch for '%s':\nExpected:\n%v\nGot:\n%v", tc.name, tc.expectedOutput, output)
			}
		})
	}
}

func TestIsBinaryFile(t *testing.T) {
	testCases := []struct {
		name        string
		fileContent string
		expect      bool
		expectError error
	}{
		{
			name:        "text file",
			fileContent: "This is a text file.",
			expect:      false,
		},
		{
			name:        "binary file with null byte at start",
			fileContent: string([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
			expect:      true,
		},
		{
			name:        "binary file with null byte in middle",
			fileContent: "This is \x00 a text file.",
			expect:      true,
		},
		{
			name:        "small text file",
			fileContent: "Short text",
			expect:      false,
		},
		{
			name:        "empty file",
			fileContent: "",
			expect:      false, // Empty file is not binary in this context
		},
		{
			name:        "error opening file",
			expect:      false, // isBinaryFile returns false on error, as per the original code's logic.
			expectError: errors.New("error opening file"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{FileContent: tc.fileContent}
			if tc.expectError != nil {
				mockFS.FileOpenError = func(name string) error {
					return errors.New("error opening file")
				}
			}

			isBinary := isBinaryFile(mockFS, "testfile.txt")

			if tc.expectError != nil {
				if isBinary != tc.expect { // Expecting false even on error
					t.Errorf("For error case '%s', expected isBinary: %v, got: %v", tc.name, tc.expect, isBinary)
				}
				return // Stop here for error cases, no error checking for isBinaryFile as per original code
			}

			if isBinary != tc.expect {
				t.Errorf("For '%s', expected isBinary: %v, got: %v", tc.name, tc.expect, isBinary)
			}
		})
	}
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
			fileContent: string([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
			isBinary:    true,
			expectOutput: `File: testfile.txt (Binary - skipped content)
--------------------------------------------------
Content of testfile.txt: (Skipped - Binary File)


`,
		},
		{
			name:        "file read error",
			fileContent: "", // Content doesn't matter if ReadFile fails
			isBinary:    false,
			expectError: errors.New("error reading file testfile.txt"),
			expectOutput: `File: testfile.txt
--------------------------------------------------
Error reading file: error reading file testfile.txt. Content skipped.
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{
				FileContent:    tc.fileContent,
				IsBinaryResult: tc.isBinary,
			}
			if tc.expectError != nil && strings.Contains(tc.name, "file read error") {
				mockFS.ReadFileError = errors.New("error reading file testfile.txt")
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

			if outputWriter.String() != tc.expectOutput {
				t.Errorf("Output mismatch for '%s':\nExpected:\n%v\nGot:\n%v", tc.name, tc.expectOutput, outputWriter.String())
			}
		})
	}
}

func TestScanFolder(t *testing.T) {
	testCases := []struct {
		name              string
		dirStructure      map[string][]string
		fileTypes         []string
		exclusionPatterns map[string]bool
		expectOutput      string
		expectError       error
	}{
		{
			name: "scan with file types filter",
			dirStructure: map[string][]string{
				"root":      {"file1.txt", "file2.log", "dir1"},
				"root/dir1": {"file3.txt", "file4.log"},
			},
			fileTypes:         []string{".txt"},
			exclusionPatterns: map[string]bool{},
			expectOutput: `Directory Structure:
-------------------
/ 
├── dir1/
└── file1.txt

File Contents:
--------------
File: dir1/file3.txt
--------------------------------------------------
Content of file3.txt:
Content of file3.txt


File: file1.txt
--------------------------------------------------
Content of file1.txt:
Content of file1.txt


`,
		},
		{
			name: "scan with exclusions and no file type filter",
			dirStructure: map[string][]string{
				"root":                   {"file1.txt", "excluded_file.txt", "dir1"},
				"root/dir1":              {"file3.txt", "excluded_dir"},
				"root/dir1/excluded_dir": {"file4.txt"},
			},
			fileTypes:         nil, // all file types
			exclusionPatterns: map[string]bool{"excluded_dir/": true, "excluded_file.txt": true},
			expectOutput: `Directory Structure:
-------------------
/ 
└── dir1/

File Contents:
--------------
File: dir1/file3.txt
--------------------------------------------------
Content of file3.txt:
Content of file3.txt


File: file1.txt
--------------------------------------------------
Content of file1.txt:
Content of file1.txt


`,
		},
		{
			name: "scan empty directory",
			dirStructure: map[string][]string{
				"root": {},
			},
			fileTypes:         nil,
			exclusionPatterns: map[string]bool{},
			expectOutput: `Directory Structure:
-------------------
/ 

File Contents:
--------------
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := &MockFS{DirStructure: tc.dirStructure, FileContent: "Content of file"} // Default file content
			outputWriter := &bytes.Buffer{}
			err := scanFolder(mockFS, "root", tc.fileTypes, outputWriter, tc.exclusionPatterns)

			if tc.expectError != nil {
				if err == nil || !strings.Contains(err.Error(), tc.expectError.Error()) {
					t.Errorf("Expected error containing: %v, got: %v", tc.expectError, err)
				}
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if outputWriter.String() != tc.expectOutput {
				t.Errorf("Output mismatch for '%s':\nExpected:\n%v\nGot:\n%v", tc.name, tc.expectOutput, outputWriter.String())
			}
		})
	}
}

// MockFS for testing
type MockFS struct {
	FileContent    string
	DirStructure   map[string][]string
	IsBinaryResult bool
	ReadFileError  error
	ReadDirError   error
	FileOpenError  func(name string) error
}

type MockFile struct {
	content   string
	closed    bool
	readError error
	openError error
}

func (m *MockFile) Read(p []byte) (n int, err error) {
	if m.readError != nil {
		return 0, m.readError
	}
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
	return []byte(m.FileContent), nil
}

func (m *MockFS) Stat(name string) (os.FileInfo, error) {
	return mockFileInfo{}, nil // Not fully implemented, adjust if Stat is used in tests
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string {
	return m.name
}
func (m mockDirEntry) IsDir() bool {
	return m.isDir
}
func (m mockDirEntry) Type() os.FileMode {
	if m.IsDir() {
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

func (m mockFileInfo) Name() string {
	return m.name
}
func (m mockFileInfo) Size() int64 {
	return 0
}
func (m mockFileInfo) Mode() os.FileMode {
	if m.isDir {
		return os.ModeDir
	}
	return 0
}
func (m mockFileInfo) ModTime() time.Time {
	return time.Now()
}
func (m mockFileInfo) IsDir() bool {
	return m.isDir
}
func (m mockFileInfo) Sys() interface{} {
	return nil
}
