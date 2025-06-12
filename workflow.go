package uobf

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ideamans/go-overwrite-batch/common"
)

// OverwriteWorkflow manages the complete overwrite batch workflow
type OverwriteWorkflow struct {
	fs             FileSystem
	statusMemory   StatusMemory
	backlogManager BacklogManager
	logger         common.Logger
}

// NewOverwriteWorkflow creates a new overwrite workflow instance
func NewOverwriteWorkflow(fs FileSystem, statusMemory StatusMemory, backlogManager BacklogManager) *OverwriteWorkflow {
	return &OverwriteWorkflow{
		fs:             fs,
		statusMemory:   statusMemory,
		backlogManager: backlogManager,
		logger:         &common.NoOpLogger{}, // Default to no-op logger
	}
}

// SetLogger sets the logger for the workflow and propagates it to sub-components
func (w *OverwriteWorkflow) SetLogger(logger common.Logger) {
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
	retryExecutor := &common.RetryExecutor{
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
func (w *OverwriteWorkflow) processFile(ctx context.Context, relPath string, retryExecutor *common.RetryExecutor, processFunc ProcessFunc) error {
	w.logger.Debug("Starting file processing", "rel_path", relPath)

	// Use the new Overwrite method with a callback
	var uploadedFileInfo *FileInfo
	overwriteErr := retryExecutor.Execute(ctx, func() error {
		var err error
		uploadedFileInfo, err = w.fs.Overwrite(ctx, relPath, func(fileInfo FileInfo, srcFilePath string) (string, bool, error) {
			// Process the downloaded file
			processedPath, processErr := processFunc(ctx, srcFilePath)
			if processErr != nil {
				w.logger.Error("Processing failed", "rel_path", relPath, "error", processErr)
				// Report error to status memory
				if reportErr := w.statusMemory.ReportError(ctx, fileInfo, processErr); reportErr != nil {
					w.logger.Warn("Failed to report process error to status memory", "error", reportErr)
				}
				return "", false, processErr
			}

			w.logger.Debug("File processed", "rel_path", relPath, "processed_path", processedPath)

			// Return the processed file path for upload
			// If processedPath is empty, it means intentional skip
			// Set autoRemove to true to clean up processed files that are different from source
			autoRemove := processedPath != "" && processedPath != srcFilePath
			return processedPath, autoRemove, nil
		})
		return err
	})

	if overwriteErr != nil {
		// Create a minimal FileInfo for error reporting if we don't have one
		if uploadedFileInfo == nil {
			fileInfo := FileInfo{
				Name:    filepath.Base(relPath),
				RelPath: relPath,
				AbsPath: relPath,
				Size:    0,
				ModTime: time.Now(),
				IsDir:   false,
			}
			if reportErr := w.statusMemory.ReportError(ctx, fileInfo, overwriteErr); reportErr != nil {
				w.logger.Warn("Failed to report error to status memory", "error", reportErr)
			}
		}
		return overwriteErr
	}

	// Report success
	if uploadedFileInfo != nil {
		w.logger.Info("File uploaded successfully", "rel_path", relPath, "size", uploadedFileInfo.Size)
		if err := w.statusMemory.ReportDone(ctx, *uploadedFileInfo); err != nil {
			w.logger.Warn("Failed to report completion", "rel_path", relPath, "error", err)
		}
	}

	return nil
}
