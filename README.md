# git2llm

A command-line tool that generates a text representation of a Git repository's structure and contents, designed for
using with large language models.

## Description

git2llm scans a directory and creates a text representation that includes:

- A visual directory tree structure
- Contents of all files (or specific file types)
- Built-in filtering to exclude binary files, secret keys, test files, dotfiles, and common directories like `.git`

## Usage

```
git2llm [options] <start_path> [file_extensions...]
```

### Arguments:

- `start_path`: The directory to scan. Typically ".".
- `file_extensions`: Optional list of file extensions to include (e.g., `.go .js .py`)

### Options:

- `-t, --exclude-tests`: Exclude test files (e.g., `*_test.go`, `*Test.java`, see test-patterns.txt in the source for a
  complete list)
- `-e`: Add pattern to exclude (e.g., `vendor` or `node_modules`). Can be used multiple times.
- `-v, --verbose`: Enable verbose output
- `-h, --help`: Display help information
- `-R`: Do not recurse into subdirectories
- `-c`: Count tokens in the output
- `-m`: Model to use for tokenization (OpenAI or Gemini models), default is "cl100k_base"

### Examples:

Scan all files in the current directory:

```
git2llm .
```

Scan only Go files in a specific directory:

```
git2llm ./src .go
```

Scan Python and JavaScript files, excluding tests:

```
git2llm --exclude-tests ./project .py .js
```

Scan all files, excluding vendor directories:

```
git2llm -e vendor .
```

Scan Go files, excluding both vendor and node_modules directories:

```
git2llm -e vendor -e node_modules . .go
```

Scan only the current directory (non-recursive):

```
git2llm -R .
```

Count tokens in the output:

```
git2llm -c .
```

Use a specific tokenization model:

```
git2llm -m gpt-4 .
```

## How It Works

1. The tool recursively traverses the specified directory
2. It generates a tree representation of the directory structure
3. For each file (filtered by extension if specified), it:
    - Checks if it's a binary file (skips if binary)
    - Checks against exclusion patterns
    - Reads and includes the file content in the output
4. Output is sent to stdout, which can be redirected to a file

## Customizing Exclusions

git2llm automatically excludes:
- Dotfiles and dotfolders (any file or folder starting with `.`)
- Common directories (`.git`, `.svn`, `.idea`, `.vscode`)
- Binary files and files containing private keys

You can create a `.llmignore` file in your project root with additional patterns to exclude.

## Installation

`go install github.com/perbu/git2llm@latest`
