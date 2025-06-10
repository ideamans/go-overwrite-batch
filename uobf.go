// Package uobf (UnifiedOverwriteBatchFlow) provides a framework for batch processing files
// across various filesystem types (local, FTP, SFTP, S3, WebDAV) with unified operations:
// 1. Scan and filter files that need processing
// 2. Download, process, and overwrite upload files in parallel
package uobf

import (
	"context"
	"time"

	"github.com/ideamans/overwritebatch/common"
)

// =============================================================================
// Core Types and Interfaces
// =============================================================================

// FileInfo represents file metadata
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
	RelPath string    `json:"rel_path"` // Relative path from the root
	AbsPath string    `json:"abs_path"` // Absolute path
}

// =============================================================================
// Filesystem Interface
// =============================================================================

// WalkOptions provides options for directory traversal
type WalkOptions struct {
	// Include patterns (open-match.dev/open-match format) - if specified, only matching files are included
	Include []string

	// Exclude patterns (open-match.dev/open-match format) - matching files are excluded
	Exclude []string

	// FollowSymlinks determines if symbolic links should be followed
	FollowSymlinks bool

	// MaxDepth limits the depth of traversal (-1 for unlimited)
	MaxDepth int

	// FilesOnly - if true, only sends files (not directories) to channel
	FilesOnly bool
}

// OverwriteCallback defines the function signature for file overwrite processing
// fileInfo: metadata of the remote file
// srcFilePath: local path where the file was downloaded
// Returns:
// - overwritingFilePath: path to the file to upload (empty string to skip upload)
// - autoRemove: if true and overwritingFilePath != "" and overwritingFilePath != srcFilePath, delete overwritingFilePath after upload
// - error: any error during processing
type OverwriteCallback func(fileInfo FileInfo, srcFilePath string) (overwritingFilePath string, autoRemove bool, err error)

// FileSystem is the main interface for filesystem operations
type FileSystem interface {
	// Close closes the filesystem connection
	Close() error

	// Walk traverses directories with options and sends FileInfo to the channel
	Walk(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error

	// Overwrite downloads a file, processes it via callback, and optionally uploads the result
	// remoteRelPath: relative path from filesystem root
	// callback: function to process the downloaded file
	// Returns the updated FileInfo and any error
	// If callback returns empty string and no error, upload is skipped (intentional skip)
	// If callback returns non-empty path, that file is uploaded to the same remote path
	Overwrite(ctx context.Context, remoteRelPath string, callback OverwriteCallback) (*FileInfo, error)

	// SetLogger sets the logger for the filesystem implementation
	SetLogger(logger common.Logger)
}

// =============================================================================
// Status Management Interface
// =============================================================================

// StatusMemory manages processing status (typically implemented with KVS)
type StatusMemory interface {
	// NeedsProcessing determines which files need processing
	// Returns a channel of FileInfo for files that need processing
	NeedsProcessing(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error)

	// ReportDone reports successful completion of file processing and stores to KVS
	ReportDone(ctx context.Context, fileInfo FileInfo) error

	// ReportError reports an error that occurred during file processing and stores to KVS
	ReportError(ctx context.Context, fileInfo FileInfo, err error) error

	// SetLogger sets the logger for the status memory
	SetLogger(logger common.Logger)
}

// =============================================================================
// Backlog Management Interface
// =============================================================================

// BacklogManager handles backlog file operations with compression
// The backlog file path is set during creation and used for all operations
type BacklogManager interface {
	// StartWriting writes relative file paths to the configured backlog file
	StartWriting(ctx context.Context, relPaths <-chan string) error

	// StartReading reads relative file paths from the configured backlog file
	StartReading(ctx context.Context) (<-chan string, error)

	// CountRelPaths counts total relative paths in the configured backlog file (for progress tracking)
	CountRelPaths(ctx context.Context) (int64, error)

	// SetLogger sets the logger for the backlog manager
	SetLogger(logger common.Logger)
}

// =============================================================================
// Processing Configuration Types
// =============================================================================

// ProcessFunc defines the function signature for file processing
type ProcessFunc func(ctx context.Context, localPath string) (processedPath string, err error)

// ProgressCallback provides progress feedback
type ProgressCallback func(processed, total int64)

// ScanAndFilterOptions provides options for the scanning and filtering phase
type ScanAndFilterOptions struct {
	// WalkOptions for filesystem traversal
	WalkOptions WalkOptions

	// BatchSize for batching file entries sent to status memory
	BatchSize int

	// EstimatedTotal for progress calculation (based on previous scan)
	EstimatedTotal int64

	// ProgressEach - report progress every N files
	ProgressEach int64

	// ProgressCallback for progress updates
	ProgressCallback ProgressCallback
}

// ProcessingOptions provides options for the processing phase
type ProcessingOptions struct {
	// Concurrency for parallel processing
	Concurrency int

	// RetryCount for download/upload operations
	RetryCount int

	// RetryDelay between retry attempts
	RetryDelay time.Duration

	// ProgressEach - report progress every N files
	ProgressEach int64

	// ProgressCallback for progress updates
	ProgressCallback ProgressCallback

	// ProcessFunc for file processing
	ProcessFunc ProcessFunc
}

// =============================================================================
// Main Workflow
// =============================================================================

// OverwriteWorkflow manages the complete overwrite batch workflow
// Implementation is in workflow.go
