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
	Path    string    `json:"path"` // Relative path from the root
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
	// Include patterns (minimatch format) - if specified, only matching files are included
	Include []string

	// Exclude patterns (minimatch format) - matching files are excluded
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
	Walk(ctx context.Context, rootPath string, options WalkOptions, ch chan<- FileInfo) error

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
type BacklogManager interface {
	// WriteBacklogFile writes file entries to a compressed backlog file
	WriteBacklogFile(ctx context.Context, filePath string, entries <-chan FileInfo) error

	// ReadBacklogFile reads file entries from a compressed backlog file
	ReadBacklogFile(ctx context.Context, filePath string) (<-chan FileInfo, error)

	// CountBacklogFile counts total entries in a backlog file (for progress tracking)
	CountBacklogFile(ctx context.Context, filePath string) (int64, error)

	// SetLogger sets the logger for the backlog manager
	SetLogger(logger Logger)
}

// =============================================================================
// Processing Configuration Types
// =============================================================================

// ProcessFunc defines the function signature for file processing
type ProcessFunc func(ctx context.Context, localPath string) (processedPath string, err error)

// ProgressCallback provides progress feedback
type ProgressCallback func(processed, total int64, phase ProcessingPhase)

type ProcessingPhase int

const (
	PhaseScanning ProcessingPhase = iota
	PhaseFiltering
	PhaseProcessing
)

// ScanAndFilterOptions provides options for the scanning and filtering phase
type ScanAndFilterOptions struct {
	// RootPath for scanning
	RootPath string

	// WalkOptions for filesystem traversal
	WalkOptions WalkOptions

	// BacklogFilePath for the compressed backlog file
	BacklogFilePath string

	// BatchSize for batching file entries sent to status memory
	BatchSize int

	// EstimatedTotal for progress calculation (based on previous scan)
	EstimatedTotal int64

	// ProgressInterval - report progress every N files
	ProgressInterval int64

	// ProgressCallback for progress updates
	ProgressCallback ProgressCallback
}

// ProcessingOptions provides options for the processing phase
type ProcessingOptions struct {
	// BacklogFilePath - path to the compressed backlog file
	BacklogFilePath string

	// TempDir for temporary file storage
	TempDir string

	// Concurrency for parallel processing
	Concurrency int

	// RetryCount for download/upload operations
	RetryCount int

	// RetryDelay between retry attempts
	RetryDelay time.Duration

	// ProgressInterval - report progress every N files
	ProgressInterval int64

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

// NetworkError represents network-related errors that should be retried
type NetworkError struct {
	Operation string
	Cause     error
}

func (e *NetworkError) Error() string {
	return e.Operation + ": " + e.Cause.Error()
}

func (e *NetworkError) IsRetryable() bool {
	return true
}

// =============================================================================
// Main Workflow
// =============================================================================

// ProcessingWorkflow manages the complete workflow
type ProcessingWorkflow struct {
	fs             FileSystem
	statusMemory   StatusMemory
	backlogManager BacklogManager
	logger         Logger
}

// NewProcessingWorkflow creates a new workflow instance
func NewProcessingWorkflow(fs FileSystem, statusMemory StatusMemory, backlogManager BacklogManager) *ProcessingWorkflow {
	return &ProcessingWorkflow{
		fs:             fs,
		statusMemory:   statusMemory,
		backlogManager: backlogManager,
		logger:         &NoOpLogger{}, // Default to no-op logger
	}
}

// SetLogger sets the logger for the workflow and propagates it to sub-components
func (w *ProcessingWorkflow) SetLogger(logger Logger) {
	w.logger = logger
	w.fs.SetLogger(logger.WithFields(map[string]interface{}{"component": "filesystem"}))
	w.statusMemory.SetLogger(logger.WithFields(map[string]interface{}{"component": "status_memory"}))
	w.backlogManager.SetLogger(logger.WithFields(map[string]interface{}{"component": "backlog_manager"}))
}

// ScanAndFilter performs the scanning and filtering phase
func (w *ProcessingWorkflow) ScanAndFilter(ctx context.Context, options ScanAndFilterOptions) error {
	w.logger.Info("Starting scan and filter phase",
		"root_path", options.RootPath,
		"backlog_file", options.BacklogFilePath,
		"estimated_total", options.EstimatedTotal)

	// Create pipeline: Walk -> Batch -> Status Memory Check -> Backlog Writer

	// Step 1: Start walking the filesystem with filtering options
	walkCh := make(chan FileInfo, options.BatchSize*2)
	go func() {
		defer close(walkCh)
		w.logger.Debug("Starting filesystem walk", "root_path", options.RootPath)
		if err := w.fs.Walk(ctx, options.RootPath, options.WalkOptions, walkCh); err != nil {
			w.logger.Error("Error during filesystem walk", "error", err)
		}
	}()

	// Step 2: Batch entries and send to status memory for processing determination
	w.logger.Debug("Starting status memory filtering")
	filteredCh, err := w.statusMemory.NeedsProcessing(ctx, walkCh)
	if err != nil {
		w.logger.Error("Error during status memory filtering", "error", err)
		return err
	}

	// Step 3: Write filtered entries to compressed backlog file
	w.logger.Debug("Writing backlog file", "path", options.BacklogFilePath)
	if err := w.backlogManager.WriteBacklogFile(ctx, options.BacklogFilePath, filteredCh); err != nil {
		w.logger.Error("Error writing backlog file", "error", err, "path", options.BacklogFilePath)
		return err
	}

	w.logger.Info("Scan and filter phase completed", "backlog_file", options.BacklogFilePath)
	return nil
}

// ProcessFiles performs the processing phase
func (w *ProcessingWorkflow) ProcessFiles(ctx context.Context, options ProcessingOptions) error {
	w.logger.Info("Starting file processing phase",
		"backlog_file", options.BacklogFilePath,
		"concurrency", options.Concurrency,
		"retry_count", options.RetryCount)

	// Step 1: Get total count for progress tracking
	total, err := w.backlogManager.CountBacklogFile(ctx, options.BacklogFilePath)
	if err != nil {
		w.logger.Error("Error counting backlog file", "error", err, "path", options.BacklogFilePath)
		return err
	}

	w.logger.Info("Backlog file loaded", "total_files", total, "path", options.BacklogFilePath)

	// Step 2: Read backlog file
	entriesCh, err := w.backlogManager.ReadBacklogFile(ctx, options.BacklogFilePath)
	if err != nil {
		w.logger.Error("Error reading backlog file", "error", err, "path", options.BacklogFilePath)
		return err
	}

	// Step 3: Process files with controlled concurrency
	w.logger.Info("Starting concurrent file processing", "workers", options.Concurrency)
	return w.processWithConcurrency(ctx, entriesCh, total, options)
}

// processWithConcurrency handles concurrent processing of files
func (w *ProcessingWorkflow) processWithConcurrency(ctx context.Context, entries <-chan FileInfo, total int64, options ProcessingOptions) error {
	// Implementation details for concurrent processing with:
	// - Download with retry logic
	// - Processing with cancellation (except during upload)
	// - Upload with retry logic and status reporting
	// - Progress reporting
	// - Error handling without stopping the workflow
	// - Status memory updates for completed and failed files
	// - Detailed logging at each step

	// This would be implemented with worker pools and careful context handling
	// to ensure upload operations complete even if context is cancelled

	// Example worker implementation with logging:
	// for each file in entries:
	//   1. logger.Debug("Starting processing", "file", fileInfo.Path)
	//   2. Download file -> logger.Debug("Downloaded", "file", fileInfo.Path, "size", downloadedSize)
	//   3. Process file -> logger.Debug("Processed", "file", fileInfo.Path, "duration", processingTime)
	//   4. Upload file -> get uploadedFileInfo -> logger.Info("Uploaded", "file", uploadedFileInfo.Path, "size", uploadedFileInfo.Size)
	//   5. Report completion: w.statusMemory.ReportDone(ctx, uploadedFileInfo)
	//   6. On error: logger.Error("Processing failed", "file", fileInfo.Path, "error", err)
	//             w.statusMemory.ReportError(ctx, originalFileInfo, err)

	w.logger.Info("File processing completed", "total_processed", total)
	return nil
}

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
