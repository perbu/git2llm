package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"github.com/perbu/git2llm/tokens"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	exclusionFile   = ".llmignore"
	secretKeyMarker = "PRIVATE KEY"
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
	Lstat(name string) (os.FileInfo, error)
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

func (OSFS) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Git2LLM represents the main application state and dependencies
type Git2LLM struct {
	fs                      FS
	outputWriter            io.Writer
	startPath               string
	fileTypes               []string
	exclusionPatterns       map[string]bool
	verbose                 bool
	excludeTests            bool
	countTokens             bool
	counter                 *tokens.Counter
	tokens                  int
	testPatternsFileContent string
	version                 string
	model                   string
}

// NewGit2LLM creates a new Git2LLM instance with the provided configuration
func NewGit2LLM(startPath string, fileTypes []string, fs FS, outputWriter io.Writer, verbose bool, excludeTests bool, countTokens bool, excludePatterns []string, model string) (*Git2LLM, error) {
	if fs == nil {
		fs = OSFS{}
	}
	if outputWriter == nil {
		outputWriter = os.Stdout
	}

	var counter *tokens.Counter
	if countTokens {
		var err error
		counter, err = tokens.New(model)
		if err != nil {
			return nil, fmt.Errorf("tokens.New(): %w", err)
		}
	}

	g := &Git2LLM{
		fs:                      fs,
		outputWriter:            outputWriter,
		startPath:               startPath,
		fileTypes:               fileTypes,
		verbose:                 verbose,
		excludeTests:            excludeTests,
		countTokens:             countTokens,
		counter:                 counter,
		testPatternsFileContent: testPatterns,
		version:                 embeddedVersion,
		model:                   model,
	}

	// Load exclusion patterns from .llmignore file
	err := g.loadExclusionPatterns(exclusionFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load exclusion patterns: %w", err)
	}

	// Add custom exclude patterns from flags
	for _, pattern := range excludePatterns {
		g.exclusionPatterns[pattern] = true
	}

	// Add test patterns if excluding tests
	if excludeTests {
		g.loadTestPatterns()
	}

	return g, nil
}

// loadTestPatterns adds test patterns to exclusion patterns
func (g *Git2LLM) loadTestPatterns() {
	testPatterns := strings.Split(g.testPatternsFileContent, "\n")
	patterns := 0
	for _, pattern := range testPatterns {
		i := strings.Index(pattern, "#")
		if i != -1 {
			pattern = pattern[:i]
		}
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			g.exclusionPatterns[pattern] = true
			patterns++
		}
	}
	if g.verbose {
		fmt.Fprintf(os.Stderr, "Excluded %d test patterns\n", patterns)
	}
}

// stringSliceFlag is a custom flag type that allows for multiple string values
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// loadExclusionPatterns reads exclusion patterns from a file.
func (g *Git2LLM) loadExclusionPatterns(filePath string) error {
	patterns := defaultPatterns()
	if filePath != "" {
		file, err := g.fs.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				g.exclusionPatterns = patterns
				return nil // Exclusion file is optional
			}
			return fmt.Errorf("error opening exclusion file: %w", err)
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
			return fmt.Errorf("error reading exclusion file: %w", err)
		}
	}
	g.exclusionPatterns = patterns
	return nil
}

// defaultPatterns returns a map of default exclusion patterns.
// the default is to ignore the .git directory.
func defaultPatterns() map[string]bool {
	return map[string]bool{
		".git":    true,
		".svn":    true,
		".idea":   true,
		".vscode": true,
		"go.sum":  true,
	}

}

// isExcluded checks if a path is excluded based on exclusion patterns.
func (g *Git2LLM) isExcluded(path string) bool {
	for pattern := range g.exclusionPatterns {
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

// generateDirectoryStructureString generates a string representation of the directory structure.
func (g *Git2LLM) generateDirectoryStructureString() (string, error) {
	var tree strings.Builder

	var generateTree func(dirPath string, prefix string) error
	generateTree = func(dirPath string, prefix string) error {
		entries, err := g.fs.ReadDir(dirPath)
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
			relPath, err := filepath.Rel(g.startPath, filepath.Join(dirPath, entryName))
			if err != nil {
				return fmt.Errorf("error getting relative path: %w", err)
			}

			if g.isExcluded(relPath) {
				continue
			}

			// Skip files that don't match fileTypes filter
			if !entry.IsDir() && g.fileTypes != nil && len(g.fileTypes) > 0 {
				var matched bool
				for _, ext := range g.fileTypes {
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
	if err := generateTree(g.startPath, ""); err != nil {
		return "", err
	}
	if g.countTokens {
		newTokens, err := g.counter.Count(tree.String())
		if err != nil {
			return "", fmt.Errorf("g.counter.Count: %w", err)
		}
		g.tokens = g.tokens + newTokens
	}

	return tree.String(), nil
}

// isSymlink checks if a file is a symbolic link.
func (g *Git2LLM) isSymlink(filePath string) bool {
	info, err := g.fs.Lstat(filePath)
	if err != nil {
		return false // Assume not symlink if error, or handle error differently
	}
	return info.Mode()&os.ModeSymlink != 0
}

// isBinaryFile checks if a file is likely a binary file by looking for null bytes in the first 16 kBytes.
func (g *Git2LLM) isForbiddenFile(filePath string) string {
	file, err := g.fs.Open(filePath)
	if err != nil {
		return fmt.Sprintf("error(open): %v", err) //
	}
	defer file.Close()

	buffer := make([]byte, 16384)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return fmt.Sprintf("error(read): %v", err) //
	}

	for i := 0; i < n; i++ {
		if buffer[i] == 0 {
			return "binary" // Found null byte, likely binary
		}
	}

	if bytes.Contains(buffer, []byte(secretKeyMarker)) {
		return "private key"
	}
	return "" // No null byte in the checked portion, likely text
}

// ScanRepository scans a folder, writes directory structure and file contents to output file.
func (g *Git2LLM) ScanRepository() error {
	if _, err := fmt.Fprintln(g.outputWriter, "Directory Structure:"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(g.outputWriter, "-------------------"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	dirTree, err := g.generateDirectoryStructureString()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(g.outputWriter, dirTree); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	if _, err := fmt.Fprintln(g.outputWriter, "\n\nFile Contents:"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(g.outputWriter, "--------------"); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	err = filepath.Walk(g.startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing path %s: %v\n", path, err) // Log to stderr
			return nil                                                         // Don't stop walking because of one error
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(g.startPath, path)
			if err != nil {
				return fmt.Errorf("error getting relative path: %w", err)
			}

			if g.isExcluded(relPath) {
				return nil
			}

			if len(g.fileTypes) == 0 { // if fileTypes is nil or empty, process all files
				if err := g.processFile(path, relPath); err != nil {
					fmt.Fprintf(os.Stderr, "Error processing file %s: %v\n", relPath, err) // Log to stderr
				}
			} else { // Otherwise check file extensions
				for _, ext := range g.fileTypes {
					if strings.HasSuffix(info.Name(), ext) {
						if err := g.processFile(path, relPath); err != nil {
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
	if g.countTokens {
		fmt.Fprintf(os.Stderr, "Total tokens: %d\n", g.tokens)
	}

	return nil
}

func (g *Git2LLM) processFile(filePath string, relPath string) error {
	if g.isSymlink(filePath) {
		fmt.Fprintf(os.Stderr, "Skipping symlink: %s\n", relPath) // Log to stderr
		if _, err := fmt.Fprintf(g.outputWriter, "File: %s (Symlink - skipped content)\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintln(g.outputWriter, strings.Repeat("-", 50)); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintf(g.outputWriter, "Content of %s: (Skipped - Symlink)\n\n\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		return nil // Skip symlinks content but not an error for overall process
	}
	reason := g.isForbiddenFile(filePath)
	if reason != "" {
		fmt.Fprintf(os.Stderr, "Skipping forbidden (%q) file: %s\n", reason, relPath) // Log to stderr
		if _, err := fmt.Fprintf(g.outputWriter, "File: %s (Binary - skipped content)\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintln(g.outputWriter, strings.Repeat("-", 50)); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		if _, err := fmt.Fprintf(g.outputWriter, "Content of %s: (Skipped - Binary File)\n\n\n", relPath); err != nil {
			return fmt.Errorf("error writing to output file: %w", err)
		}
		return nil // Skip binary files content but not an error for overall process
	}

	if g.verbose {
		fmt.Fprintf(os.Stderr, "Processing: %s ", relPath) // Log to stderr
	}

	if _, err := fmt.Fprintf(g.outputWriter, "File: %s\n", relPath); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(g.outputWriter, strings.Repeat("-", 50)); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	content, err := g.fs.ReadFile(filePath)
	if err != nil {
		if _, errWrite := fmt.Fprintf(g.outputWriter, "Error reading file: %s. Content skipped.\n", err); errWrite != nil {
			return fmt.Errorf("error writing error message to output file: %w (original error: %v)", errWrite, err)
		}
		return fmt.Errorf("error reading file %s: %w", relPath, err) // Still return an error for logging in scanFolder
	}
	var newTokens int
	if g.countTokens {
		var err error
		newTokens, err = g.counter.Count(string(content))
		if err != nil {
			return fmt.Errorf("g.counter.Count: %w", err)
		}
		g.tokens = g.tokens + newTokens
	}

	if g.verbose {
		// count the number of lines in the file
		lineCount := strings.Count(string(content), "\n")
		switch g.countTokens {
		case true:
			fmt.Fprintf(os.Stderr, "(%d tokens, %d lines)\n", newTokens, lineCount) // Log to stderr
		default:
			fmt.Fprintf(os.Stderr, "(%d lines)\n", lineCount) // Log to stderr
		}

	}
	if _, err := fmt.Fprintf(g.outputWriter, "Content of %s:\n", relPath); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := g.outputWriter.Write(content); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(g.outputWriter); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if _, err := fmt.Fprintln(g.outputWriter); err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	if g.countTokens {
		newTokens, err := g.counter.Count(string(content))
		if err != nil {
			return fmt.Errorf("g.counter.Count: %w", err)
		}
		g.tokens = g.tokens + newTokens
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

	var countTokens bool
	flag.BoolVar(&countTokens, "c", false, "Count tokens in the output")

	var excludePatterns stringSliceFlag
	flag.Var(&excludePatterns, "e", "Add pattern to exclude (e.g., vendor)")

	var model string
	flag.StringVar(&model, "m", "cl100k_base", "Model to use (OpenAI or Gemini models)")

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

	// Create Git2LLM instance
	git2llm, err := NewGit2LLM(startPath, fileTypes, nil, os.Stdout, verbose, excludeTests, countTokens, excludePatterns, model)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error initializing git2llm: %v\n", err)
		}
		os.Exit(1)
	}

	// Add patterns from -e flags
	if len(excludePatterns) > 0 && verbose {
		fmt.Fprintf(os.Stderr, "Added %d custom exclusion patterns\n", len(excludePatterns))
	}

	if !excludeTests && verbose {
		fmt.Fprintf(os.Stderr, "Including all files.\n")
	}

	err = git2llm.ScanRepository()
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
