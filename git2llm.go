package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	exclusionFile = ".llmignore"
)

//go:embed test-patterns.txt
var testPatterns string

//go:embed .version
var embeddedVersion string

// FS defines an interface for file system operations to improve testability.
type FS interface {
	Open(name string) (File, error)
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Stat(name string) (os.FileInfo, error)
}

// File defines an interface for file operations, mirroring os.File methods we use.
type File interface {
	io.ReadCloser
}

// OSFS implements the FS interface using the standard os package.
type OSFS struct{}

func (OSFS) Open(name string) (File, error) {
	return os.Open(name)
}

func (OSFS) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (OSFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (OSFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// FileSystem is a global variable for the FS interface.
// This allows for easy swapping of the implementation in tests.
var FileSystem FS = OSFS{}

// stringSliceFlag is a custom flag type that allows for multiple string values
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// parseExclusionFile reads exclusion patterns from a file.
func parseExclusionFile(fs FS, filePath string) (map[string]bool, error) {
	patterns := defaultPatterns()
	if filePath != "" {
		file, err := fs.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				return patterns, nil // Exclusion file is optional
			}
			return nil, fmt.Errorf("error opening exclusion file: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns[line] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading exclusion file: %w", err)
		}
	}
	return patterns, nil
}

// defaultPatterns returns a map of default exclusion patterns.
// the default is to ignore the .git directory.
func defaultPatterns() map[string]bool {
	return map[string]bool{
		".git":    true,
		".svn":    true,
		".idea":   true,
		".vscode": true,
	}

}

// isExcluded checks if a path is excluded based on exclusion patterns.
func isExcluded(path string, exclusionPatterns map[string]bool) bool {
	for pattern := range exclusionPatterns {
		if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(path, pattern[1:]) || path == pattern[1:len(pattern)-1] {
				return true
			}
		} else if strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(path, pattern) || path == pattern[:len(pattern)-1] {
				return true
			}
		} else if strings.HasPrefix(pattern, "/") {
			if path == pattern[1:] || strings.HasPrefix(path, pattern[1:]+string(os.PathSeparator)) {
				return true
			}
		} else {
			if matched, _ := filepath.Match(pattern, path); matched {
				return true
			}
			parts := strings.Split(path, string(os.PathSeparator))
			for _, part := range parts {
				if matched, _ := filepath.Match(pattern, part); matched {
					return true
				}
			}
		}
	}
	return false
}

// printDirectoryStructure generates a string representation of the directory structure.
func printDirectoryStructure(fs FS, startPath string, exclusionPatterns map[string]bool, fileTypes []string) (string, error) {
	var tree strings.Builder

	var generateTree func(dirPath string, prefix string) error
	generateTree = func(dirPath string, prefix string) error {
		entries, err := fs.ReadDir(dirPath)
		if err != nil {
			return fmt.Errorf("error reading directory: %w", err)
		}

		// Sort entries: directories first, then alphabetically
		sort.Slice(entries, func(i, j int) bool {
			isDirI := entries[i].IsDir()
			isDirJ := entries[j].IsDir()
			if isDirI != isDirJ {
				return isDirI // Directories first
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) // Then alphabetical
		})

		for i, entry := range entries {
			entryName := entry.Name()
			relPath, err := filepath.Rel(startPath, filepath.Join(dirPath, entryName))
			if err != nil {
				return fmt.Errorf("error getting relative path: %w", err)
			}

			if isExcluded(relPath, exclusionPatterns) {
				continue
			}

			// Skip files that don't match fileTypes filter
			if !entry.IsDir() && fileTypes != nil && len(fileTypes) > 0 {
				var matched bool
				for _, ext := range fileTypes {
					if strings.HasSuffix(entryName, ext) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			var connector string
			var newPrefix string
			if i == len(entries)-1 {
				connector = "└── "
				newPrefix = prefix + "    "
			} else {
				connector = "├── "
				newPrefix = prefix + "│   "
			}

			fullPath := filepath.Join(dirPath, entryName)
			if entry.IsDir() {
				if _, err := fmt.Fprintf(&tree, "%s%s%s/\n", prefix, connector, entryName); err != nil {
					return fmt.Errorf("error writing to tree string: %w", err)
				}
				if err := generateTree(fullPath, newPrefix); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(&tree, "%s%s%s\n", prefix, connector, entryName); err != nil {
					return fmt.Errorf("error writing to tree string: %w", err)
				}
			}
		}
		return nil
	}

	if _, err := fmt.Fprintf(&tree, "/ \n"); err != nil {
		return "", fmt.Errorf("error writing to tree string: %w", err)
	}
	if err := generateTree(startPath, ""); err != nil {
		return "", err
	}

	return tree.String(), nil
}

// isSymlinkFn is a variable holding the function to check if a file is a symbolic link.
// This allows for easy swapping of the implementation in tests.
var isSymlinkFn = func(filePath string) bool {
	info, err := os.Lstat(filePath)
	if err != nil {
		return false // Assume not symlink if error, or handle error differently
	}
	return info.Mode()&os.ModeSymlink != 0
}

// isSymlink checks if a file is a symbolic link.
func isSymlink(filePath string) bool {
	return isSymlinkFn(filePath)
}

// isBinaryFile checks if a file is likely a binary file by looking for null bytes in the first 16 kBytes.
func isBinaryFile(fs FS, filePath string) bool {
	file, err := fs.Open(filePath)
	if err != nil {
		return false // Assume not binary if error opening, or handle error differently
	}
	defer file.Close()

	buffer := make([]byte, 16384)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false // Assume not binary if read error, or handle error differently
	}

	for i := 0; i < n; i++ {
		if buffer[i] == 0 {
			return true // Found null byte, likely binary
		}
	}
	return false // No null byte in the checked portion, likely text
}

// scanFolder scans a folder, writes directory structure and file contents to output file.
func scanFolder(fs FS, startPath string, fileTypes []string, outputWriter io.Writer, exclusionPatterns map[string]bool, verbose bool) error {

	if _, err := fmt.Fprintln(outputWriter, "Directory Structure:"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(outputWriter, "-------------------"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	dirTree, err := printDirectoryStructure(fs, startPath, exclusionPatterns, fileTypes)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(outputWriter, dirTree); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	if _, err := fmt.Fprintln(outputWriter, "\n\nFile Contents:"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(outputWriter, "--------------"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	err = filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing path %s: %v\n", path, err) // Log to stderr
			return nil                                                         // Don't stop walking because of one error
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(startPath, path)
			if err != nil {
				return fmt.Errorf("error getting relative path: %w", err)
			}

			if isExcluded(relPath, exclusionPatterns) {
				return nil
			}

			if len(fileTypes) == 0 { // if fileTypes is nil or empty, process all files
				if err := processFile(fs, outputWriter, path, relPath, verbose); err != nil {
					fmt.Fprintf(os.Stderr, "Error processing file %s: %v\n", relPath, err) // Log to stderr
				}
			} else { // Otherwise check file extensions
				for _, ext := range fileTypes {
					if strings.HasSuffix(info.Name(), ext) {
						if err := processFile(fs, outputWriter, path, relPath, verbose); err != nil {
							fmt.Fprintf(os.Stderr, "Error processing file %s: %v\n", relPath, err) // Log to stderr
						}
						return nil // processed the file, no need to check other extensions
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	return nil
}

func processFile(fs FS, outputWriter io.Writer, filePath string, relPath string, verbose bool) error {
	if isSymlink(filePath) {
		fmt.Fprintf(os.Stderr, "Skipping symlink: %s\n", relPath) // Log to stderr
		if _, err := fmt.Fprintf(outputWriter, "File: %s (Symlink - skipped content)\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintln(outputWriter, strings.Repeat("-", 50)); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintf(outputWriter, "Content of %s: (Skipped - Symlink)\n\n\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		return nil // Skip symlinks content but not an error for overall process
	}

	if isBinaryFile(fs, filePath) {
		fmt.Fprintf(os.Stderr, "Skipping binary file: %s\n", relPath) // Log to stderr
		if _, err := fmt.Fprintf(outputWriter, "File: %s (Binary - skipped content)\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintln(outputWriter, strings.Repeat("-", 50)); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintf(outputWriter, "Content of %s: (Skipped - Binary File)\n\n\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		return nil // Skip binary files content but not an error for overall process
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Processing: %s ", relPath) // Log to stderr
	}

	if _, err := fmt.Fprintf(outputWriter, "File: %s\n", relPath); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(outputWriter, strings.Repeat("-", 50)); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	content, err := fs.ReadFile(filePath)
	if err != nil {
		if _, errWrite := fmt.Fprintf(outputWriter, "Error reading file: %s. Content skipped.\n", err); errWrite != nil {
			return fmt.Errorf("error writing error message to output file: %w (original error: %v)", errWrite, err)
		}
		return fmt.Errorf("error reading file %s: %w", relPath, err) // Still return an error for logging in scanFolder
	}
	if verbose {
		// count the number of lines in the file
		lineCount := strings.Count(string(content), "\n")
		fmt.Fprintf(os.Stderr, "(%d lines)\n", lineCount) // Log to stderr
	}
	if _, err := fmt.Fprintf(outputWriter, "Content of %s:\n", relPath); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := outputWriter.Write(content); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(outputWriter); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(outputWriter); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	return nil
}

func printUsage() {
	fmt.Printf("Usage: %s [options] <start_path> [file_extensions...]\n\n", os.Args[0])
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println("\nArguments:")
	fmt.Println("  start_path             Path to the directory to scan")
	fmt.Println("  file_extensions        Optional file extensions to include (e.g., .go .js)")
}

func main() {
	// Define flags
	var excludeTests bool
	flag.BoolVar(&excludeTests, "t", false, "Exclude test files from known languages")
	flag.BoolVar(&excludeTests, "exclude-tests", false, "Exclude test files from known languages")

	var verbose bool
	flag.BoolVar(&verbose, "v", false, "Enable verbose output")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")

	var excludePatterns stringSliceFlag
	flag.Var(&excludePatterns, "e", "Add pattern to exclude (e.g., vendor)")

	var help bool
	flag.BoolVar(&help, "h", false, "Display this help message")
	flag.BoolVar(&help, "help", false, "Display this help message")

	// Override default usage function
	flag.Usage = printUsage

	// Parse flags
	flag.Parse()

	// Check if help flag is set
	if help {
		printUsage()
		os.Exit(0)
	}

	// Get remaining arguments after flags
	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	startPath := args[0]

	if verbose {
		fmt.Fprintf(os.Stderr, "Version: %s\n", embeddedVersion)
	}

	exclusionPatterns, err := parseExclusionFile(FileSystem, exclusionFile)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error parsing exclusion file: %v\n", err)
		}
		os.Exit(1)
	}

	if excludeTests {
		testPatterns := strings.Split(testPatterns, "\n")
		patterns := 0
		for _, pattern := range testPatterns {
			i := strings.Index(pattern, "#")
			if i != -1 {
				pattern = pattern[:i]
			}
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				exclusionPatterns[pattern] = true
				patterns++
			}
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "Excluded %d test patterns\n", patterns)
		}
	} else {
		if verbose {
			fmt.Fprintf(os.Stderr, "Including all files.\n")
		}
	}

	// Add patterns from -e flags
	if len(excludePatterns) > 0 {
		for _, pattern := range excludePatterns {
			exclusionPatterns[pattern] = true
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "Added %d custom exclusion patterns\n", len(excludePatterns))
		}
	}

	var fileTypes []string
	if len(args) > 1 {
		fileTypes = args[1:]
	}

	if verbose {
		if fileTypes != nil {
			fmt.Fprintf(os.Stderr, "Scanning for file types: %v\n", fileTypes)
		} else {
			fmt.Fprintf(os.Stderr, "No file types specified. Scanning all files.\n")
		}
	}

	err = scanFolder(FileSystem, startPath, fileTypes, os.Stdout, exclusionPatterns, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
		}
		os.Exit(1)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Scan complete.")
	}
}
