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

# Format code
go fmt ./...

# Vet code for potential issues  
go vet ./...

# Check module dependencies
go mod tidy
```

# Architecture

The system implements a two-phase workflow:

## Core Components

- **ProcessingWorkflow**: Central orchestrator managing the complete workflow
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
BacklogManager.ReadBacklogFile() → Worker Pool → Download → Process → Upload → Status Update
```

See `plan/architecture.md` for detailed architecture documentation.

# Code Structure

The main interface definitions are in `uobf.go:1-379`. Key interfaces:

- `FileSystem` interface: `uobf.go:68-83` - Unified filesystem operations
- `StatusMemory` interface: `uobf.go:90-103` - Processing state management  
- `BacklogManager` interface: `uobf.go:110-122` - Compressed backlog file handling
- `ProcessingWorkflow` struct: `uobf.go:222-227` - Main workflow orchestrator

The project uses a plugin architecture where different implementations of core interfaces can be swapped (filesystem types, status storage backends, etc.).

# File Organization

See `plan/filemap.md` for the planned directory structure. Implementation follows a package-per-concern pattern:

- `filesystem/` - Storage protocol implementations
- `status/` - State management backends 
- `backlog/` - Backlog file formats
- `internal/` - Private utilities (worker pools, retry logic, progress tracking)

# Dependencies

- **Pattern Matching**: Uses `open-match.dev/open-match` for minimatch-style file pattern filtering in `WalkOptions.Include` and `WalkOptions.Exclude`

# Error Handling

Uses `RetryableError` interface (`uobf.go:198-201`) to distinguish network errors that should be retried. Individual file processing failures don't stop the overall workflow.

# Logging Integration

All components implement `Logger` interface (`uobf.go:28-43`) compatible with popular Go logging libraries. Supports structured logging with contextual fields.
