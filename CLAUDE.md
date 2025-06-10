# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Project Overview

UnifiedOverwriteBatchFlow (UOBF) - A Go library providing unified batch processing across various filesystem types (local, FTP, SFTP, S3, WebDAV). Enables efficient scanning, filtering, downloading, processing, and overwrite uploading of large file sets.

This is an early-stage project being actively developed with Claude Code.

# Development Commands

```bash
# Build the module
go build ./...

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test ./filesystem

# Run integration tests with Docker containers
go test -tags=integration ./tests/integration/...

# Format code
go fmt ./...

# Vet code for potential issues  
go vet ./...

# Check module dependencies
go mod tidy
```

# Testing Strategy

## Unit Tests

Standard Go unit tests for individual components and interfaces.

## Integration Tests with Docker

Remote filesystem implementations are tested using Docker containers:

- **SFTP**: Uses `atmoz/sftp` container for SSH/SFTP server simulation
- **FTP**: Uses `stilliard/pure-ftpd` container for FTP server testing  
- **WebDAV**: Uses `bytemark/webdav` container for WebDAV protocol testing
- **S3**: Uses `minio/minio` container for S3-compatible API testing

Integration tests are tagged with `//go:build integration` and run separately to avoid Docker dependency in regular unit tests.

# Architecture

The system implements a two-phase workflow:

## Core Components

- **OverwriteWorkflow**: Central orchestrator managing the complete overwrite batch workflow
- **FileSystem**: Interface abstracting different storage protocols (Local, FTP, SFTP, S3, WebDAV)
- **StatusMemory**: Tracks file processing state using KVS to prevent duplicate processing
- **BacklogManager**: Manages compressed backlog files containing lists of files to process

## Data Flow

**Phase 1: Scan & Filter**

```
FileSystem.Walk() → StatusMemory.NeedsProcessing() → BacklogManager.WriteBacklogFile()
```

**Phase 2: Process Files**  

```
BacklogManager.ReadBacklogFile() → Worker Pool → FileSystem.Overwrite() → Status Update
```

The `Overwrite` method combines download, process, and upload operations in a single atomic operation with callback-based processing.

See `plan/architecture.md` for detailed architecture documentation.

# Code Structure

The project follows a clean architecture with shared utilities in the `common` package:

## Core Interfaces (uobf.go)

- `FileSystem` interface - Unified filesystem operations with `Walk` and `Overwrite` methods
  - `Walk`: Traverses directories with filtering options
  - `Overwrite`: Combines download, process via callback, and optional upload in one atomic operation
- `OverwriteCallback` type - Function signature for processing downloaded files
  - Returns processed file path, autoRemove flag, and error
  - If autoRemove is true and processed path differs from source, the processed file is deleted after upload
- `StatusMemory` interface - Processing state management  
- `BacklogManager` interface - Compressed backlog file handling
- `OverwriteWorkflow` struct - Main workflow orchestrator (implementation in workflow.go)

## Common Utilities (common/utils.go)

- `Logger` interface - Structured logging interface compatible with popular Go loggers
- `NoOpLogger` - No-operation logger for testing or when logging is disabled
- `RetryExecutor` - Retry logic for network operations with exponential backoff
- `RetryableError` interface - Distinguishes retryable errors from permanent failures
- `NetworkError` - Network-specific error with retry capability

## Implementations

- `backlog/gzip.go` - Gzip-compressed text backlog manager
- `backlog/memory.go` - In-memory backlog manager for testing
- `filesystem/local.go` - Local filesystem implementation
- `status/memory.go` - In-memory status memory implementation
- `workflow.go` - Complete OverwriteWorkflow implementation

The project uses a plugin architecture where different implementations of core interfaces can be swapped (filesystem types, status storage backends, etc.).

# File Organization

See `plan/filemap.md` for the planned directory structure. Implementation follows a package-per-concern pattern:

- `filesystem/` - Storage protocol implementations
- `status/` - State management backends
- `backlog/` - Backlog file formats (gzip-compressed JSON implementation)
- `l10n/` - Localization infrastructure for multi-language support
- `internal/` - Private utilities (worker pools, retry logic, progress tracking)

# Dependencies

- **Pattern Matching**: Uses `open-match.dev/open-match` for minimatch-style file pattern filtering in `WalkOptions.Include` and `WalkOptions.Exclude`
- **Status Storage**: Uses LevelDB (`github.com/syndtr/goleveldb`) for embedded key-value storage of processing status
- **Localization**: Uses `golang.org/x/text/language` for language detection and matching
- **Docker Testing**: Uses `github.com/testcontainers/testcontainers-go` for Docker container management and `github.com/ory/dockertest/v3` as alternative testing helper

# Error Handling

Uses `RetryableError` interface (`uobf.go:198-201`) to distinguish network errors that should be retried. Individual file processing failures don't stop the overall workflow.

# Logging Integration

All components implement `Logger` interface (`uobf.go:28-43`) compatible with popular Go logging libraries. Supports structured logging with contextual fields.

# Localization (l10n)

The project includes a comprehensive localization infrastructure (`l10n/` package) to support multiple languages for log messages, error messages, and user-facing text.

## Core Features

- **Automatic Language Detection**: Detects user's preferred language from environment variables (`LANGUAGE`, `LC_ALL`, `LC_MESSAGES`, `LANG`)
- **Translation Function**: Simple `T(phrase)` function for translating text
- **Extensible Registration**: Allows registration of custom translations via `Register(lang, lexicon)`
- **Fallback Support**: Returns original phrase if translation is not available

## Supported Languages

- **English (en)**: Default language
- **Japanese (ja)**: Primary additional language support

## Usage Guidelines

**IMPORTANT**: All components should actively use the l10n package for user-facing messages:

### In Log Messages

```go
import "github.com/ideamans/go-l10n"

// Use T() function for all log messages
logger.Info(l10n.T("Starting file processing"), "count", fileCount)
logger.Error(l10n.T("Failed to connect to filesystem"), "error", err)
```

### In Error Messages

```go
// Use T() for error messages that may be shown to users
return fmt.Errorf(l10n.T("file not found: %s"), filename)
return &NetworkError{
    Operation: l10n.T("download"),
    Cause: err,
}
```

### Registering Custom Translations

```go
// Register translations in init function to avoid duplicate registration
func init() {
    l10n.Register("ja", l10n.LexiconMap{
        "Starting file processing": "ファイル処理を開始します",
        "Failed to connect to filesystem": "ファイルシステムへの接続に失敗しました",
        "file not found: %s": "ファイルが見つかりません: %s",
        "download": "ダウンロード",
    })
}
```

## Implementation Requirements

**All new code must:**

1. Import and use the `github.com/ideamans/go-l10n` package for any user-facing text
2. Wrap log messages and error strings with `l10n.T()`
3. Register appropriate translations for Japanese (ja) language in `init()` function
4. Use English as the base phrase in `T()` calls
5. Keep phrases concise and context-appropriate

**For existing code:**

- Gradually migrate existing hardcoded strings to use `l10n.T()`
- Prioritize user-facing errors and important log messages
- Add Japanese translations for commonly seen messages

## Language Detection

The system automatically detects the user's language preference on initialization:

- Checks environment variables in order: `LANGUAGE`, `LC_ALL`, `LC_MESSAGES`, `LANG`
- Uses `golang.org/x/text/language` for robust language matching
- Defaults to English if no supported language is detected
- Currently supports Japanese and English language tags

## Test Mode Language Handling

**IMPORTANT**: During test execution, the language is automatically fixed to English (`"en"`) to ensure consistent test results:

- **Automatic Test Detection**: The system detects test mode by checking command-line arguments for `.test`, `-test.`, or `_test` patterns
- **Language Consistency**: All log messages and error messages use English during tests, regardless of environment variables
- **Manual Control**: Use `l10n.ForceLanguage()` for specific test scenarios requiring different languages

### Testing Guidelines

```go
func TestWithJapanese(t *testing.T) {
    // Force Japanese for specific test scenarios
    l10n.ForceLanguage("ja")
    defer l10n.ResetLanguage() // Reset to automatic detection
    
    // Test code that expects Japanese messages
    result := l10n.T("File processing completed successfully")
    assert.Equal(t, "ファイル処理が正常に完了しました", result)
}

func TestNormalOperation(t *testing.T) {
    // Normal tests automatically use English
    // No special setup required
    result := l10n.T("File processing completed successfully") 
    assert.Equal(t, "File processing completed successfully", result)
}
```

This localization infrastructure ensures UOBF can be effectively used by international teams and in diverse deployment environments while maintaining test reliability.

# BacklogManager Implementation

The BacklogManager handles compressed storage of file lists for batch processing. The current implementation uses gzip compression for space efficiency.

## GzipBacklogManager

Located in `backlog/gzip.go`, provides:

- **Gzip Compression**: Reduces backlog file size significantly for large file lists
- **JSON Serialization**: Uses JSON for structured data storage within compressed files
- **Stream Processing**: Handles large backlogs without loading entire content into memory
- **Progress Tracking**: Provides periodic progress updates during long operations
- **Context Cancellation**: Supports graceful cancellation of read/write operations
- **Buffered I/O**: Uses buffered readers/writers for improved performance

### Usage Example

```go
backlogPath := "/tmp/processing.backlog.gz"
manager := backlog.NewGzipBacklogManager(backlogPath)
manager.SetLogger(logger)

// Writing relative paths
relPathsChan := make(chan string, 100)
go func() {
    defer close(relPathsChan)
    relPathsChan <- "file1.txt"
    relPathsChan <- "dir/file2.txt"
    // Add more relative paths...
}()
err := manager.StartWriting(ctx, relPathsChan)

// Reading relative paths
readChan, err := manager.StartReading(ctx)
for relPath := range readChan {
    // Process relative path...
}

// Count relative paths for progress tracking
count, err := manager.CountRelPaths(ctx)
```

### File Format

Backlog files use gzip-compressed text with one relative path per line:

```
file1.txt
dir/file2.txt
subdir/file3.txt
```

# Graceful Shutdown

The system supports graceful shutdown with special handling for upload operations:

- **Upload Protection**: File uploads cannot be interrupted mid-operation to prevent corruption
- **Context Cancellation**: Most operations respect context cancellation except during upload phase
- **Worker Pool Shutdown**: Workers complete current tasks before terminating
- **Resource Cleanup**: Filesystem connections and temporary files are properly cleaned up

# important-instruction-reminders

Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.

## Localization Requirements

ALWAYS use the l10n package for any user-facing text in code:

- Import `github.com/ideamans/overwritebatch/l10n` in all components
- Wrap all log messages with `l10n.T("message")`
- Wrap all error messages with `l10n.T("error message")`
- Register Japanese translations using `l10n.Register("ja", l10n.LexiconMap{...})` in `init()` function
- Use English as the base phrase in all `l10n.T()` calls
