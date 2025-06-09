# Basic Example

This example demonstrates the basic usage of OverwriteBatch library with:
- Local filesystem for both source and destination
- LevelDB for status tracking
- Gzip-compressed backlog files

## Features Demonstrated

- File filtering with include/exclude patterns
- Concurrent processing with configurable workers
- Simple file transformation (uppercase conversion or timestamp header)
- Progress logging

## Usage

```bash
# Build the example
go build -o basic-example

# Run with default transformation (adds timestamp header)
./basic-example -source /path/to/source -dest /path/to/dest

# Run with uppercase transformation
./basic-example -source /path/to/source -dest /path/to/dest -uppercase

# Run with custom patterns and workers
./basic-example -source /path/to/source -dest /path/to/dest \
  -include "*.txt,*.log,*.md" \
  -exclude "temp/*,*.tmp" \
  -workers 8
```

## Command Line Options

- `-source`: Source directory path (required)
- `-dest`: Destination directory path (required)
- `-status`: Status database path (default: `/tmp/overwritebatch-status.db`)
- `-backlog`: Backlog file path (default: `/tmp/overwritebatch-backlog.gz`)
- `-include`: Comma-separated include patterns (default: `*.txt,*.log`)
- `-exclude`: Comma-separated exclude patterns (default: empty)
- `-workers`: Number of concurrent workers (default: 4)
- `-uppercase`: Convert content to uppercase (default: false)

## Example Test Script

See `test.sh` for an automated test that:
1. Creates a test environment with sample files
2. Runs the example with various options
3. Verifies the results
4. Cleans up test data