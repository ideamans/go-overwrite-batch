package uobf

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ideamans/overwritebatch/common"
	"github.com/ideamans/go-l10n"
)

func init() {
	// Register Japanese translations for workflow operations
	l10n.Register("ja", l10n.LexiconMap{
		"Starting scan and filter phase":       "スキャンとフィルタフェーズを開始します",
		"Starting filesystem walk":             "ファイルシステムウォークを開始します",
		"Error during filesystem walk":         "ファイルシステムウォーク中にエラーが発生しました",
		"Starting status memory filtering":     "ステータスメモリフィルタリングを開始します",
		"Error during status memory filtering": "ステータスメモリフィルタリング中にエラーが発生しました",
		"Writing backlog file":                 "バックログファイルを書き込み中です",
		"Error writing backlog file":           "バックログファイルの書き込み中にエラーが発生しました",
		"Filesystem walk error detected":       "ファイルシステムウォークエラーが検出されました",
		"Scan and filter phase completed":      "スキャンとフィルタフェーズが完了しました",
		"Starting file processing phase":       "ファイル処理フェーズを開始します",
		"Error counting backlog entries":       "バックログエントリの集計中にエラーが発生しました",
		"Backlog file loaded":                  "バックログファイルが読み込まれました",
		"Error reading backlog file":           "バックログファイルの読み取り中にエラーが発生しました",
		"Starting concurrent file processing":  "並行ファイル処理を開始します",
		"Failed to process file":               "ファイルの処理に失敗しました",
		"File processing completed":            "ファイル処理が完了しました",
		"Starting file processing":             "ファイル処理を開始します",
		"Download failed":                      "ダウンロードに失敗しました",
		"File downloaded":                      "ファイルがダウンロードされました",
		"Processing failed":                    "処理に失敗しました",
		"File processed":                       "ファイルが処理されました",
		"Upload failed":                        "アップロードに失敗しました",
		"File uploaded successfully":           "ファイルが正常にアップロードされました",
		"Failed to report completion":          "完了報告に失敗しました",
	})
}

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
	w.logger.Info(l10n.T("Starting scan and filter phase"),
		"estimated_total", options.EstimatedTotal)

	// Create pipeline: Walk -> Batch -> Status Memory Check -> Backlog Writer

	// Step 1: Start walking the filesystem with filtering options
	w.logger.Debug(l10n.T("Starting filesystem walk"))
	walkCh := make(chan FileInfo, options.BatchSize*2)
	walkErr := make(chan error, 1)

	go func() {
		defer close(walkCh)
		defer close(walkErr)
		if err := w.fs.Walk(ctx, options.WalkOptions, walkCh); err != nil {
			w.logger.Error(l10n.T("Error during filesystem walk"), "error", err)
			walkErr <- err
		}
	}()

	// Step 2: Batch entries and send to status memory for processing determination
	w.logger.Debug(l10n.T("Starting status memory filtering"))
	filteredCh, err := w.statusMemory.NeedsProcessing(ctx, walkCh)
	if err != nil {
		w.logger.Error(l10n.T("Error during status memory filtering"), "error", err)
		return err
	}

	// Step 3: Extract relative paths and write to compressed backlog file
	w.logger.Debug(l10n.T("Writing backlog file"))
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
		w.logger.Error(l10n.T("Error writing backlog file"), "error", err)
		return err
	}

	// Check for any walk errors that occurred during processing
	select {
	case walkErrResult := <-walkErr:
		if walkErrResult != nil {
			w.logger.Error(l10n.T("Filesystem walk error detected"), "error", walkErrResult)
			return walkErrResult
		}
	default:
		// No error
	}

	w.logger.Info(l10n.T("Scan and filter phase completed"))
	return nil
}

// ProcessFiles performs the processing phase
func (w *OverwriteWorkflow) ProcessFiles(ctx context.Context, options ProcessingOptions) error {
	w.logger.Info(l10n.T("Starting file processing phase"),
		"concurrency", options.Concurrency,
		"retry_count", options.RetryCount)

	// Step 1: Get total count for progress tracking
	total, err := w.backlogManager.CountRelPaths(ctx)
	if err != nil {
		w.logger.Error(l10n.T("Error counting backlog entries"), "error", err)
		return err
	}

	w.logger.Info(l10n.T("Backlog file loaded"), "total_files", total)

	// Step 2: Read backlog file (relative paths)
	relPathsCh, err := w.backlogManager.StartReading(ctx)
	if err != nil {
		w.logger.Error(l10n.T("Error reading backlog file"), "error", err)
		return err
	}

	// Step 3: Convert relative paths back to FileInfo and process files
	w.logger.Info(l10n.T("Starting concurrent file processing"), "workers", options.Concurrency)
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
					w.logger.Error(l10n.T("Failed to process file"), "rel_path", relPath, "worker", workerID, "error", err)
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

	w.logger.Info(l10n.T("File processing completed"), "total_processed", processed, "total_expected", total)
	return nil
}

// processFile handles the processing of a single file
func (w *OverwriteWorkflow) processFile(ctx context.Context, relPath string, retryExecutor *common.RetryExecutor, processFunc ProcessFunc) error {
	w.logger.Debug(l10n.T("Starting file processing"), "rel_path", relPath)

	// Create FileInfo for the relative path
	fileInfo := FileInfo{
		Name:    filepath.Base(relPath),
		RelPath: relPath,
		AbsPath: relPath, // For LocalFileSystem, this should be treated as the remote path
		Size:    0,       // Would be populated by filesystem.Stat in real implementation
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
		w.logger.Error(l10n.T("Download failed"), "rel_path", relPath, "error", downloadErr)
		w.statusMemory.ReportError(ctx, fileInfo, downloadErr)
		return downloadErr
	}

	w.logger.Debug(l10n.T("File downloaded"), "rel_path", relPath, "temp_path", tempPath)

	// Process file
	processedPath, processErr := processFunc(ctx, tempPath)
	if processErr != nil {
		w.logger.Error(l10n.T("Processing failed"), "rel_path", relPath, "error", processErr)
		w.statusMemory.ReportError(ctx, fileInfo, processErr)
		return processErr
	}

	w.logger.Debug(l10n.T("File processed"), "rel_path", relPath, "processed_path", processedPath)

	// Upload with retry (no cancellation during upload)
	var uploadedFileInfo *FileInfo
	uploadErr := retryExecutor.Execute(context.Background(), func() error {
		var err error
		uploadedFileInfo, err = w.fs.Upload(ctx, processedPath, relPath)
		return err
	})
	if uploadErr != nil {
		w.logger.Error(l10n.T("Upload failed"), "rel_path", relPath, "error", uploadErr)
		w.statusMemory.ReportError(ctx, fileInfo, uploadErr)
		return uploadErr
	}

	w.logger.Info(l10n.T("File uploaded successfully"), "rel_path", relPath, "size", uploadedFileInfo.Size)

	// Report success
	if err := w.statusMemory.ReportDone(ctx, *uploadedFileInfo); err != nil {
		w.logger.Warn(l10n.T("Failed to report completion"), "rel_path", relPath, "error", err)
	}

	return nil
}
