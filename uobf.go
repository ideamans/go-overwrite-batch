// Package uobf (UnifiedOverwriteBatchFlow) provides a framework for batch processing files
// across various filesystem types (local, FTP, SFTP, S3, WebDAV) with unified operations:
// 1. Scan and filter files that need processing
// 2. Download, process, and overwrite upload files in parallel
package uobf

import (
	"context"
	"time"
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

// Logger provides a standard logging interface
// Compatible with popular loggers like logrus, zap, slog, etc.
type Logger interface {
	// Debug logs debug-level messages
	Debug(msg string, fields ...interface{})

	// Info logs info-level messages
	Info(msg string, fields ...interface{})

	// Warn logs warning-level messages
	Warn(msg string, fields ...interface{})

	// Error logs error-level messages
	Error(msg string, fields ...interface{})

	// WithFields returns a logger with additional fields
	WithFields(fields map[string]interface{}) Logger
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

// FileSystem is the main interface for filesystem operations
type FileSystem interface {
	// Close closes the filesystem connection
	Close() error

	// Walk traverses directories with options and sends FileInfo to the channel
	Walk(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error

	// Download downloads a file from the filesystem to local path
	Download(ctx context.Context, remotePath, localPath string) error

	// Upload uploads a local file to the filesystem and returns the uploaded file info
	Upload(ctx context.Context, localPath, remotePath string) (*FileInfo, error)

	// SetLogger sets the logger for the filesystem implementation
	SetLogger(logger Logger)
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
	SetLogger(logger Logger)
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
	SetLogger(logger Logger)
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
// Error Types
// =============================================================================

// RetryableError indicates if an error should be retried
type RetryableError interface {
	error
	IsRetryable() bool
}

// NetworkError represents network-related errors that may be retried
type NetworkError struct {
	Operation   string
	Cause       error
	ShouldRetry bool
}

func (e *NetworkError) Error() string {
	return e.Operation + ": " + e.Cause.Error()
}

func (e *NetworkError) IsRetryable() bool {
	return e.ShouldRetry
}

// =============================================================================
// Main Workflow
// =============================================================================

// OverwriteWorkflow manages the complete overwrite batch workflow
// Implementation is in workflow.go

// =============================================================================
// Utility Implementations
// =============================================================================

// NoOpLogger provides a no-operation logger for testing or when logging is disabled
type NoOpLogger struct{}

func (l *NoOpLogger) Debug(msg string, fields ...interface{})         {}
func (l *NoOpLogger) Info(msg string, fields ...interface{})          {}
func (l *NoOpLogger) Warn(msg string, fields ...interface{})          {}
func (l *NoOpLogger) Error(msg string, fields ...interface{})         {}
func (l *NoOpLogger) WithFields(fields map[string]interface{}) Logger { return l }

// RetryExecutor handles retry logic for network operations
type RetryExecutor struct {
	MaxRetries int
	Delay      time.Duration
}

func (r *RetryExecutor) Execute(ctx context.Context, operation func() error) error {
	var lastErr error
	for i := 0; i <= r.MaxRetries; i++ {
		if err := operation(); err != nil {
			if retryable, ok := err.(RetryableError); ok && retryable.IsRetryable() && i < r.MaxRetries {
				lastErr = err
				select {
				case <-time.After(r.Delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}
		return nil
	}
	return lastErr
}
