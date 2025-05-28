// Package uobf (UnifiedOverwriteBatchFlow) provides a framework for batch processing files
// across various filesystem types (local, FTP, SFTP, S3, WebDAV) with unified operations:
// 1. Scan and filter files that need processing
// 2. Download, process, and overwrite upload files in parallel
package uobf

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync/atomic"
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
type OverwriteWorkflow struct {
	fs             FileSystem
	statusMemory   StatusMemory
	backlogManager BacklogManager
	logger         Logger
}

// NewOverwriteWorkflow creates a new overwrite workflow instance
func NewOverwriteWorkflow(fs FileSystem, statusMemory StatusMemory, backlogManager BacklogManager) *OverwriteWorkflow {
	return &OverwriteWorkflow{
		fs:             fs,
		statusMemory:   statusMemory,
		backlogManager: backlogManager,
		logger:         &NoOpLogger{}, // Default to no-op logger
	}
}

// SetLogger sets the logger for the workflow and propagates it to sub-components
func (w *OverwriteWorkflow) SetLogger(logger Logger) {
	w.logger = logger
	w.fs.SetLogger(logger.WithFields(map[string]interface{}{"component": "filesystem"}))
	w.statusMemory.SetLogger(logger.WithFields(map[string]interface{}{"component": "status_memory"}))
	w.backlogManager.SetLogger(logger.WithFields(map[string]interface{}{"component": "backlog_manager"}))
}

// ScanAndFilter performs the scanning and filtering phase
func (w *OverwriteWorkflow) ScanAndFilter(ctx context.Context, options ScanAndFilterOptions) error {
	w.logger.Info("Starting scan and filter phase",
		"estimated_total", options.EstimatedTotal)

	// Create pipeline: Walk -> Batch -> Status Memory Check -> Backlog Writer

	// Step 1: Start walking the filesystem with filtering options
	w.logger.Debug("Starting filesystem walk")
	walkCh := make(chan FileInfo, options.BatchSize*2)
	walkErr := make(chan error, 1)
	
	go func() {
		defer close(walkCh)
		defer close(walkErr)
		if err := w.fs.Walk(ctx, options.WalkOptions, walkCh); err != nil {
			w.logger.Error("Error during filesystem walk", "error", err)
			walkErr <- err
		}
	}()

	// Step 2: Batch entries and send to status memory for processing determination
	w.logger.Debug("Starting status memory filtering")
	filteredCh, err := w.statusMemory.NeedsProcessing(ctx, walkCh)
	if err != nil {
		w.logger.Error("Error during status memory filtering", "error", err)
		return err
	}

	// Step 3: Extract relative paths and write to compressed backlog file
	w.logger.Debug("Writing backlog file")
	relPathCh := make(chan string, 100)
	
	// Start goroutine to convert FileInfo to relative paths
	go func() {
		defer close(relPathCh)
		for fileInfo := range filteredCh {
			select {
			case <-ctx.Done():
				return
			case relPathCh <- fileInfo.RelPath:
			}
		}
	}()
	
	if err := w.backlogManager.StartWriting(ctx, relPathCh); err != nil {
		w.logger.Error("Error writing backlog file", "error", err)
		return err
	}

	// Check for any walk errors that occurred during processing
	select {
	case walkErrResult := <-walkErr:
		if walkErrResult != nil {
			w.logger.Error("Filesystem walk error detected", "error", walkErrResult)
			return walkErrResult
		}
	default:
		// No error
	}

	w.logger.Info("Scan and filter phase completed")
	return nil
}

// ProcessFiles performs the processing phase
func (w *OverwriteWorkflow) ProcessFiles(ctx context.Context, options ProcessingOptions) error {
	w.logger.Info("Starting file processing phase",
		"concurrency", options.Concurrency,
		"retry_count", options.RetryCount)

	// Step 1: Get total count for progress tracking
	total, err := w.backlogManager.CountRelPaths(ctx)
	if err != nil {
		w.logger.Error("Error counting backlog entries", "error", err)
		return err
	}

	w.logger.Info("Backlog file loaded", "total_files", total)

	// Step 2: Read backlog file (relative paths)
	relPathsCh, err := w.backlogManager.StartReading(ctx)
	if err != nil {
		w.logger.Error("Error reading backlog file", "error", err)
		return err
	}

	// Step 3: Convert relative paths back to FileInfo and process files
	w.logger.Info("Starting concurrent file processing", "workers", options.Concurrency)
	return w.processWithConcurrency(ctx, relPathsCh, total, options)
}

// processWithConcurrency handles concurrent processing of files
func (w *OverwriteWorkflow) processWithConcurrency(ctx context.Context, relPaths <-chan string, total int64, options ProcessingOptions) error {
	// Create worker pool
	workerChan := make(chan string, options.Concurrency*2) // Buffer for workers
	errChan := make(chan error, options.Concurrency)
	doneChan := make(chan struct{})
	
	var processed int64
	retryExecutor := &RetryExecutor{
		MaxRetries: options.RetryCount,
		Delay:      options.RetryDelay,
	}
	
	// Start workers
	for i := 0; i < options.Concurrency; i++ {
		go func(workerID int) {
			defer func() { doneChan <- struct{}{} }()
			
			for relPath := range workerChan {
				if err := w.processFile(ctx, relPath, retryExecutor, options.ProcessFunc); err != nil {
					w.logger.Error("Failed to process file", "rel_path", relPath, "worker", workerID, "error", err)
					errChan <- err
				} else {
					// Progress reporting
					current := atomic.AddInt64(&processed, 1)
					if options.ProgressCallback != nil && options.ProgressEach > 0 && current%options.ProgressEach == 0 {
						options.ProgressCallback(current, total)
					}
				}
			}
		}(i)
	}
	
	// Feed work to workers
	go func() {
		defer close(workerChan)
		for relPath := range relPaths {
			select {
			case <-ctx.Done():
				return
			case workerChan <- relPath:
			}
		}
	}()
	
	// Wait for all workers to complete
	for i := 0; i < options.Concurrency; i++ {
		<-doneChan
	}
	
	// Final progress report
	if options.ProgressCallback != nil {
		options.ProgressCallback(processed, total)
	}
	
	w.logger.Info("File processing completed", "total_processed", processed, "total_expected", total)
	return nil
}

// processFile handles the processing of a single file
func (w *OverwriteWorkflow) processFile(ctx context.Context, relPath string, retryExecutor *RetryExecutor, processFunc ProcessFunc) error {
	w.logger.Debug("Starting file processing", "rel_path", relPath)
	
	// Create FileInfo for the relative path (simplified - would need filesystem.Stat in real implementation)
	fileInfo := FileInfo{
		Name:    filepath.Base(relPath),
		RelPath: relPath,
		AbsPath: filepath.Join("/", relPath), // Simplified absolute path
		Size:    0,                           // Would be populated by filesystem.Stat
		ModTime: time.Now(),
		IsDir:   false,
	}
	
	// Create temporary file for download
	tempFile, err := ioutil.TempFile("", "uobf_*"+filepath.Ext(relPath))
	if err != nil {
		w.statusMemory.ReportError(ctx, fileInfo, err)
		return err
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)
	
	// Download with retry
	downloadErr := retryExecutor.Execute(ctx, func() error {
		return w.fs.Download(ctx, relPath, tempPath)
	})
	if downloadErr != nil {
		w.logger.Error("Download failed", "rel_path", relPath, "error", downloadErr)
		w.statusMemory.ReportError(ctx, fileInfo, downloadErr)
		return downloadErr
	}
	
	w.logger.Debug("File downloaded", "rel_path", relPath, "temp_path", tempPath)
	
	// Process file
	processedPath, processErr := processFunc(ctx, tempPath)
	if processErr != nil {
		w.logger.Error("Processing failed", "rel_path", relPath, "error", processErr)
		w.statusMemory.ReportError(ctx, fileInfo, processErr)
		return processErr
	}
	
	w.logger.Debug("File processed", "rel_path", relPath, "processed_path", processedPath)
	
	// Upload with retry (no cancellation during upload)
	var uploadedFileInfo *FileInfo
	uploadErr := retryExecutor.Execute(context.Background(), func() error {
		var err error
		uploadedFileInfo, err = w.fs.Upload(ctx, processedPath, relPath)
		return err
	})
	if uploadErr != nil {
		w.logger.Error("Upload failed", "rel_path", relPath, "error", uploadErr)
		w.statusMemory.ReportError(ctx, fileInfo, uploadErr)
		return uploadErr
	}
	
	w.logger.Info("File uploaded successfully", "rel_path", relPath, "size", uploadedFileInfo.Size)
	
	// Report success
	if err := w.statusMemory.ReportDone(ctx, *uploadedFileInfo); err != nil {
		w.logger.Warn("Failed to report completion", "rel_path", relPath, "error", err)
	}
	
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
