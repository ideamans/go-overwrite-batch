package uobf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
)

// Mock implementations for testing

// MockFileSystem provides a mock implementation of FileSystem
type MockFileSystem struct {
	WalkFunc     func(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error
	DownloadFunc func(ctx context.Context, remoteRelPath, localFullPath string) error
	UploadFunc   func(ctx context.Context, localFullPath, remoteRelPath string) (*FileInfo, error)
	CloseFunc    func() error
	SetLoggerFunc func(logger common.Logger)
}

func (m *MockFileSystem) Walk(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error {
	if m.WalkFunc != nil {
		return m.WalkFunc(ctx, options, ch)
	}
	close(ch)
	return nil
}

func (m *MockFileSystem) Download(ctx context.Context, remoteRelPath, localFullPath string) error {
	if m.DownloadFunc != nil {
		return m.DownloadFunc(ctx, remoteRelPath, localFullPath)
	}
	return nil
}

func (m *MockFileSystem) Upload(ctx context.Context, localFullPath, remoteRelPath string) (*FileInfo, error) {
	if m.UploadFunc != nil {
		return m.UploadFunc(ctx, localFullPath, remoteRelPath)
	}
	return &FileInfo{
		Name:    "test.txt",
		RelPath: remoteRelPath, 
		AbsPath: "/abs" + remoteRelPath, 
		Size:    1024, 
		ModTime: time.Now(),
	}, nil
}

func (m *MockFileSystem) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockFileSystem) SetLogger(logger common.Logger) {
	if m.SetLoggerFunc != nil {
		m.SetLoggerFunc(logger)
	}
}

// MockStatusMemory provides a mock implementation of StatusMemory
type MockStatusMemory struct {
	NeedsProcessingFunc func(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error)
	ReportDoneFunc      func(ctx context.Context, fileInfo FileInfo) error
	ReportErrorFunc     func(ctx context.Context, fileInfo FileInfo, err error) error
	SetLoggerFunc       func(logger common.Logger)
}

func (m *MockStatusMemory) NeedsProcessing(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error) {
	if m.NeedsProcessingFunc != nil {
		return m.NeedsProcessingFunc(ctx, entries)
	}
	
	// Default: pass through all entries
	filtered := make(chan FileInfo, 100)
	go func() {
		defer close(filtered)
		for entry := range entries {
			select {
			case <-ctx.Done():
				return
			case filtered <- entry:
			}
		}
	}()
	return filtered, nil
}

func (m *MockStatusMemory) ReportDone(ctx context.Context, fileInfo FileInfo) error {
	if m.ReportDoneFunc != nil {
		return m.ReportDoneFunc(ctx, fileInfo)
	}
	return nil
}

func (m *MockStatusMemory) ReportError(ctx context.Context, fileInfo FileInfo, err error) error {
	if m.ReportErrorFunc != nil {
		return m.ReportErrorFunc(ctx, fileInfo, err)
	}
	return nil
}

func (m *MockStatusMemory) SetLogger(logger common.Logger) {
	if m.SetLoggerFunc != nil {
		m.SetLoggerFunc(logger)
	}
}

// MockBacklogManager provides a mock implementation of BacklogManager
type MockBacklogManager struct {
	StartWritingFunc  func(ctx context.Context, relPaths <-chan string) error
	StartReadingFunc  func(ctx context.Context) (<-chan string, error)
	CountRelPathsFunc func(ctx context.Context) (int64, error)
	SetLoggerFunc     func(logger common.Logger)
}

func (m *MockBacklogManager) StartWriting(ctx context.Context, relPaths <-chan string) error {
	if m.StartWritingFunc != nil {
		return m.StartWritingFunc(ctx, relPaths)
	}
	
	// Default: consume all entries
	for range relPaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func (m *MockBacklogManager) StartReading(ctx context.Context) (<-chan string, error) {
	if m.StartReadingFunc != nil {
		return m.StartReadingFunc(ctx)
	}
	
	// Default: return empty channel
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *MockBacklogManager) CountRelPaths(ctx context.Context) (int64, error) {
	if m.CountRelPathsFunc != nil {
		return m.CountRelPathsFunc(ctx)
	}
	return 0, nil
}

func (m *MockBacklogManager) SetLogger(logger common.Logger) {
	if m.SetLoggerFunc != nil {
		m.SetLoggerFunc(logger)
	}
}

// Test helper functions

func createTestWorkflow() (*OverwriteWorkflow, *MockFileSystem, *MockStatusMemory, *MockBacklogManager) {
	fs := &MockFileSystem{}
	status := &MockStatusMemory{}
	backlog := &MockBacklogManager{}
	workflow := NewOverwriteWorkflow(fs, status, backlog)
	return workflow, fs, status, backlog
}

func createTestFileInfos(count int) []FileInfo {
	files := make([]FileInfo, count)
	baseTime := time.Now()
	for i := 0; i < count; i++ {
		fileName := "file" + string(rune(i+'0')) + ".txt"
		files[i] = FileInfo{
			Name:    fileName,
			RelPath: fileName,
			AbsPath: "/root/" + fileName,
			Size:    int64(1000 + i),
			ModTime: baseTime.Add(time.Duration(i) * time.Second),
			IsDir:   false,
		}
	}
	return files
}

// Test data and helpers for specific scenarios

var (
	testError = errors.New("test error")
	retryableTestError = &common.NetworkError{
		Operation:   "test operation",
		Cause:       errors.New("network timeout"),
		ShouldRetry: true,
	}
	nonRetryableTestError = &common.NetworkError{
		Operation:   "test operation", 
		Cause:       errors.New("authentication failed"),
		ShouldRetry: false,
	}
)

// Progress tracking helper for tests
type progressTracker struct {
	calls []progressCall
}

type progressCall struct {
	processed int64
	total     int64
}

func (pt *progressTracker) callback(processed, total int64) {
	pt.calls = append(pt.calls, progressCall{processed: processed, total: total})
}

func (pt *progressTracker) reset() {
	pt.calls = nil
}

func (pt *progressTracker) callCount() int {
	return len(pt.calls)
}

func (pt *progressTracker) lastCall() progressCall {
	if len(pt.calls) == 0 {
		return progressCall{}
	}
	return pt.calls[len(pt.calls)-1]
}

// Actual tests start here

func TestOverwriteWorkflow_ScanAndFilter_Success(t *testing.T) {
	workflow, fs, status, backlog := createTestWorkflow()
	
	// Mock FileSystem to return test files
	testFiles := createTestFileInfos(3)
	fs.WalkFunc = func(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error {
		for _, file := range testFiles {
			ch <- file
		}
		return nil
	}
	
	// Mock StatusMemory to pass through all files
	status.NeedsProcessingFunc = func(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error) {
		filtered := make(chan FileInfo, 10)
		go func() {
			defer close(filtered)
			for entry := range entries {
				filtered <- entry
			}
		}()
		return filtered, nil
	}
	
	// Track what gets written to backlog
	var writtenPaths []string
	backlog.StartWritingFunc = func(ctx context.Context, relPaths <-chan string) error {
		for relPath := range relPaths {
			writtenPaths = append(writtenPaths, relPath)
		}
		return nil
	}
	
	// Execute
	options := ScanAndFilterOptions{
		WalkOptions: WalkOptions{
			Include: []string{"*.txt"},
		},
	}
	
	err := workflow.ScanAndFilter(context.Background(), options)
	
	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	
	if len(writtenPaths) != len(testFiles) {
		t.Errorf("Expected %d paths written to backlog, got %d", len(testFiles), len(writtenPaths))
	}
	
	for i, expectedFile := range testFiles {
		if i < len(writtenPaths) && writtenPaths[i] != expectedFile.RelPath {
			t.Errorf("Expected path %s at index %d, got %s", expectedFile.RelPath, i, writtenPaths[i])
		}
	}
}

func TestOverwriteWorkflow_ScanAndFilter_FileSystemError(t *testing.T) {
	workflow, fs, _, _ := createTestWorkflow()
	
	// Mock FileSystem to return error
	fs.WalkFunc = func(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error {
		return testError
	}
	
	// Execute
	options := ScanAndFilterOptions{
		WalkOptions: WalkOptions{
			Include: []string{"*.txt"},
		},
	}
	
	err := workflow.ScanAndFilter(context.Background(), options)
	
	// Verify
	if err != testError {
		t.Errorf("Expected testError, got: %v", err)
	}
}

func TestOverwriteWorkflow_ScanAndFilter_StatusMemoryError(t *testing.T) {
	workflow, fs, status, _ := createTestWorkflow()
	
	// Mock FileSystem to return test files
	testFiles := createTestFileInfos(2)
	fs.WalkFunc = func(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error {
		for _, file := range testFiles {
			ch <- file
		}
		return nil
	}
	
	// Mock StatusMemory to return error
	status.NeedsProcessingFunc = func(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error) {
		// Consume entries to avoid blocking
		go func() {
			for range entries {
			}
		}()
		return nil, testError
	}
	
	// Execute
	options := ScanAndFilterOptions{}
	err := workflow.ScanAndFilter(context.Background(), options)
	
	// Verify
	if err != testError {
		t.Errorf("Expected testError, got: %v", err)
	}
}

func TestOverwriteWorkflow_ScanAndFilter_BacklogManagerError(t *testing.T) {
	workflow, fs, status, backlog := createTestWorkflow()
	
	// Mock FileSystem to return test files
	testFiles := createTestFileInfos(2)
	fs.WalkFunc = func(ctx context.Context, options WalkOptions, ch chan<- FileInfo) error {
		for _, file := range testFiles {
			ch <- file
		}
		return nil
	}
	
	// Mock StatusMemory to pass through all files
	status.NeedsProcessingFunc = func(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error) {
		filtered := make(chan FileInfo, 10)
		go func() {
			defer close(filtered)
			for entry := range entries {
				filtered <- entry
			}
		}()
		return filtered, nil
	}
	
	// Mock BacklogManager to return error
	backlog.StartWritingFunc = func(ctx context.Context, relPaths <-chan string) error {
		// Consume to avoid blocking
		for range relPaths {
		}
		return testError
	}
	
	// Execute
	options := ScanAndFilterOptions{}
	err := workflow.ScanAndFilter(context.Background(), options)
	
	// Verify
	if err != testError {
		t.Errorf("Expected testError, got: %v", err)
	}
}

func TestOverwriteWorkflow_ProcessFiles_Success(t *testing.T) {
	workflow, fs, status, backlog := createTestWorkflow()
	
	// Test data
	testPaths := []string{"file1.txt", "file2.txt"}
	
	// Mock BacklogManager to return test paths
	backlog.CountRelPathsFunc = func(ctx context.Context) (int64, error) {
		return int64(len(testPaths)), nil
	}
	
	backlog.StartReadingFunc = func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, len(testPaths))
		for _, path := range testPaths {
			ch <- path
		}
		close(ch)
		return ch, nil
	}
	
	// Mock FileSystem operations
	fs.DownloadFunc = func(ctx context.Context, remoteRelPath, localFullPath string) error {
		return nil // Success
	}
	
	fs.UploadFunc = func(ctx context.Context, localFullPath, remoteRelPath string) (*FileInfo, error) {
		return &FileInfo{
			Name:    "test.txt",
			RelPath: remoteRelPath,
			AbsPath: "/abs/" + remoteRelPath,
			Size:    1024,
			ModTime: time.Now(),
		}, nil
	}
	
	// Mock StatusMemory operations
	var reportedFiles []FileInfo
	status.ReportDoneFunc = func(ctx context.Context, fileInfo FileInfo) error {
		reportedFiles = append(reportedFiles, fileInfo)
		return nil
	}
	
	// Progress tracking
	tracker := &progressTracker{}
	
	// Execute
	options := ProcessingOptions{
		Concurrency:      2,
		RetryCount:       3,
		RetryDelay:       10 * time.Millisecond,
		ProgressEach:     1,
		ProgressCallback: tracker.callback,
		ProcessFunc: func(ctx context.Context, localPath string) (string, error) {
			return localPath, nil // Simple pass-through
		},
	}
	
	err := workflow.ProcessFiles(context.Background(), options)
	
	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	
	if len(reportedFiles) != len(testPaths) {
		t.Errorf("Expected %d files reported as done, got %d", len(testPaths), len(reportedFiles))
	}
	
	if tracker.callCount() == 0 {
		t.Error("Expected progress callbacks to be called")
	}
	
	lastCall := tracker.lastCall()
	if lastCall.processed != int64(len(testPaths)) {
		t.Errorf("Expected final progress to show %d processed, got %d", len(testPaths), lastCall.processed)
	}
}

func TestOverwriteWorkflow_ProcessFiles_DownloadError(t *testing.T) {
	workflow, fs, status, backlog := createTestWorkflow()
	
	// Test data
	testPaths := []string{"file1.txt"}
	
	// Mock BacklogManager
	backlog.CountRelPathsFunc = func(ctx context.Context) (int64, error) {
		return int64(len(testPaths)), nil
	}
	
	backlog.StartReadingFunc = func(ctx context.Context) (<-chan string, error) {
		ch := make(chan string, len(testPaths))
		for _, path := range testPaths {
			ch <- path
		}
		close(ch)
		return ch, nil
	}
	
	// Mock FileSystem to fail download
	fs.DownloadFunc = func(ctx context.Context, remoteRelPath, localFullPath string) error {
		return testError
	}
	
	// Mock StatusMemory operations
	var errorReports []FileInfo
	status.ReportErrorFunc = func(ctx context.Context, fileInfo FileInfo, err error) error {
		errorReports = append(errorReports, fileInfo)
		return nil
	}
	
	// Execute
	options := ProcessingOptions{
		Concurrency: 1,
		RetryCount:  0, // No retries to make test faster
		ProcessFunc: func(ctx context.Context, localPath string) (string, error) {
			return localPath, nil
		},
	}
	
	err := workflow.ProcessFiles(context.Background(), options)
	
	// Verify - workflow should complete despite file errors
	if err != nil {
		t.Fatalf("Expected no error from workflow, got: %v", err)
	}
	
	if len(errorReports) != len(testPaths) {
		t.Errorf("Expected %d error reports, got %d", len(testPaths), len(errorReports))
	}
}

func TestOverwriteWorkflow_ProcessFiles_BacklogManagerError(t *testing.T) {
	workflow, _, _, backlog := createTestWorkflow()
	
	// Mock BacklogManager to fail counting
	backlog.CountRelPathsFunc = func(ctx context.Context) (int64, error) {
		return 0, testError
	}
	
	// Execute
	options := ProcessingOptions{
		Concurrency: 1,
		ProcessFunc: func(ctx context.Context, localPath string) (string, error) {
			return localPath, nil
		},
	}
	
	err := workflow.ProcessFiles(context.Background(), options)
	
	// Verify
	if err != testError {
		t.Errorf("Expected testError, got: %v", err)
	}
}

func TestOverwriteWorkflow_SetLogger(t *testing.T) {
	workflow, fs, status, backlog := createTestWorkflow()
	
	// Create test logger
	testLogger := &common.NoOpLogger{}
	
	// Mock SetLogger calls to verify they're called
	var fsLoggerSet, statusLoggerSet, backlogLoggerSet bool
	
	fs.SetLoggerFunc = func(logger common.Logger) {
		fsLoggerSet = true
	}
	
	status.SetLoggerFunc = func(logger common.Logger) {
		statusLoggerSet = true
	}
	
	backlog.SetLoggerFunc = func(logger common.Logger) {
		backlogLoggerSet = true
	}
	
	// Execute
	workflow.SetLogger(testLogger)
	
	// Verify
	if !fsLoggerSet {
		t.Error("Expected FileSystem.SetLogger to be called")
	}
	
	if !statusLoggerSet {
		t.Error("Expected StatusMemory.SetLogger to be called")
	}
	
	if !backlogLoggerSet {
		t.Error("Expected BacklogManager.SetLogger to be called")
	}
}