# OverwriteBatch

A Go library providing unified batch processing across various filesystem types. It enables efficient scanning, filtering, downloading, processing, and overwrite uploading of large file sets.

## Features

- **Unified Filesystem Interface**: Support for Local, FTP, SFTP, S3, and WebDAV filesystems
- **Efficient Batch Processing**: Two-phase workflow for optimal performance
- **State Management**: Prevents duplicate processing using persistent status tracking
- **Compressed Backlogs**: Gzip-compressed file lists for efficient storage
- **Concurrent Processing**: Configurable worker pools for parallel file operations
- **Retry Logic**: Built-in retry mechanism with exponential backoff
- **Internationalization**: Support for multiple languages (English and Japanese)

## Installation

```bash
go get github.com/ideamans/overwritebatch
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    
    uobf "github.com/ideamans/overwritebatch"
    "github.com/ideamans/overwritebatch/backlog"
    "github.com/ideamans/overwritebatch/filesystem"
    "github.com/ideamans/overwritebatch/status"
)

func main() {
    // Create filesystem instances
    fs := filesystem.NewLocalFileSystem("/source/path")
    
    // Create status memory with LevelDB
    statusMem, err := status.NewLevelDBStatusMemory("/tmp/status.db")
    if err != nil {
        log.Fatal(err)
    }
    defer statusMem.Close()
    
    // Create backlog manager
    backlogMgr := backlog.NewGzipBacklogManager("/tmp/backlog.gz")
    
    // Create workflow
    workflow := &uobf.OverwriteWorkflow{
        SourceFS:       fs,
        DestFS:         fs,
        StatusMemory:   statusMem,
        BacklogManager: backlogMgr,
        Concurrency:    4,
    }
    
    // Define walk options
    walkOpts := &uobf.WalkOptions{
        Include: []string{"*.txt", "*.log"},
        Exclude: []string{"temp/*"},
    }
    
    // Define process function
    processFn := func(ctx context.Context, relPath string, content []byte) ([]byte, error) {
        // Your processing logic here
        return content, nil
    }
    
    // Execute workflow
    ctx := context.Background()
    err = workflow.Execute(ctx, walkOpts, processFn)
    if err != nil {
        log.Fatal(err)
    }
}
```

## Architecture

The library implements a two-phase workflow:

### Phase 1: Scan & Filter
- Walks through the source filesystem
- Filters files based on include/exclude patterns
- Checks processing status to avoid duplicates
- Writes file paths to compressed backlog

### Phase 2: Process Files
- Reads file paths from backlog
- Downloads files in parallel using worker pool
- Applies user-defined processing function
- Uploads processed files to destination
- Updates processing status

## Components

### FileSystem Interface
Provides unified operations across different storage types:
- `Walk`: Traverse directory structure with filtering options
- `Overwrite`: Download, process via callback, and optionally upload files in one atomic operation
  - Callback receives file metadata and downloaded file path
  - Returns processed file path, autoRemove flag, and error
  - If processed path is empty, upload is skipped (intentional skip)
  - If autoRemove is true and processed path differs from source, the processed file is automatically deleted after upload
  - Supports error handling and graceful cleanup

### StatusMemory Interface
Tracks file processing state:
- `NeedsProcessing`: Check if file should be processed
- `MarkAsProcessed`: Record successful processing
- `MarkAsFailed`: Record processing failure

### BacklogManager Interface
Manages compressed file lists:
- `StartWriting`: Write file paths to backlog
- `StartReading`: Read file paths from backlog
- `CountRelPaths`: Count total files for progress tracking

## Examples

See the [examples/basic](examples/basic) directory for a complete working example.

## Testing

```bash
# Run unit tests
go test ./...

# Run integration tests (requires Docker)
go test -tags=integration ./tests/integration/...
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.