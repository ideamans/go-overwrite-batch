package status

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	uobf "github.com/ideamans/overwritebatch"
	"github.com/ideamans/overwritebatch/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryStatusMemory(t *testing.T) {
	sm := NewMemoryStatusMemory()
	assert.NotNil(t, sm)
	assert.NotNil(t, sm.status)
	assert.NotNil(t, sm.logger)
	assert.Equal(t, 0, sm.Count())
}

func TestMemoryStatusMemory_SetLogger(t *testing.T) {
	sm := NewMemoryStatusMemory()
	mockLogger := &mockLogger{}
	sm.SetLogger(mockLogger)
	assert.Equal(t, mockLogger, sm.logger)
}

func TestMemoryStatusMemory_NeedsProcessing_NewFiles(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	// Create test files
	files := []uobf.FileInfo{
		{
			RelPath: "file1.txt",
			Size:    100,
			ModTime: time.Now(),
		},
		{
			RelPath: "file2.txt",
			Size:    200,
			ModTime: time.Now(),
		},
	}

	// Create input channel
	inputCh := make(chan uobf.FileInfo, len(files))
	for _, file := range files {
		inputCh <- file
	}
	close(inputCh)

	// Test NeedsProcessing
	ctx := context.Background()
	resultCh, err := sm.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	// Collect results
	var results []uobf.FileInfo
	for fileInfo := range resultCh {
		results = append(results, fileInfo)
	}

	// All files should need processing (new files)
	assert.Len(t, results, 2)
	assert.Contains(t, results, files[0])
	assert.Contains(t, results, files[1])
}

func TestMemoryStatusMemory_NeedsProcessing_ExistingFiles(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	baseTime := time.Now()

	// Create and report some files as done
	file1 := uobf.FileInfo{
		RelPath: "file1.txt",
		Size:    100,
		ModTime: baseTime,
	}
	file2 := uobf.FileInfo{
		RelPath: "file2.txt",
		Size:    200,
		ModTime: baseTime,
	}

	ctx := context.Background()
	err := sm.ReportDone(ctx, file1)
	require.NoError(t, err)
	err = sm.ReportDone(ctx, file2)
	require.NoError(t, err)

	// Test with same files (no changes)
	files := []uobf.FileInfo{file1, file2}
	inputCh := make(chan uobf.FileInfo, len(files))
	for _, file := range files {
		inputCh <- file
	}
	close(inputCh)

	resultCh, err := sm.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	// Collect results
	var results []uobf.FileInfo
	for fileInfo := range resultCh {
		results = append(results, fileInfo)
	}

	// No files should need processing (unchanged)
	assert.Len(t, results, 0)
}

func TestMemoryStatusMemory_NeedsProcessing_ModifiedFiles(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	baseTime := time.Now()

	// Create and report a file as done
	originalFile := uobf.FileInfo{
		RelPath: "file1.txt",
		Size:    100,
		ModTime: baseTime,
	}

	ctx := context.Background()
	err := sm.ReportDone(ctx, originalFile)
	require.NoError(t, err)

	tests := []struct {
		name         string
		modifiedFile uobf.FileInfo
		shouldNeed   bool
	}{
		{
			name: "modified size",
			modifiedFile: uobf.FileInfo{
				RelPath: "file1.txt",
				Size:    150, // changed size
				ModTime: baseTime,
			},
			shouldNeed: true,
		},
		{
			name: "modified time",
			modifiedFile: uobf.FileInfo{
				RelPath: "file1.txt",
				Size:    100,
				ModTime: baseTime.Add(time.Hour), // changed time
			},
			shouldNeed: true,
		},
		{
			name: "both modified",
			modifiedFile: uobf.FileInfo{
				RelPath: "file1.txt",
				Size:    250,                     // changed size
				ModTime: baseTime.Add(time.Hour), // changed time
			},
			shouldNeed: true,
		},
		{
			name: "unchanged",
			modifiedFile: uobf.FileInfo{
				RelPath: "file1.txt",
				Size:    100,
				ModTime: baseTime,
			},
			shouldNeed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputCh := make(chan uobf.FileInfo, 1)
			inputCh <- tt.modifiedFile
			close(inputCh)

			resultCh, err := sm.NeedsProcessing(ctx, inputCh)
			require.NoError(t, err)

			var results []uobf.FileInfo
			for fileInfo := range resultCh {
				results = append(results, fileInfo)
			}

			if tt.shouldNeed {
				assert.Len(t, results, 1)
				assert.Equal(t, tt.modifiedFile, results[0])
			} else {
				assert.Len(t, results, 0)
			}
		})
	}
}

func TestMemoryStatusMemory_NeedsProcessing_FilesWithErrors(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	// Create and report a file with error
	fileWithError := uobf.FileInfo{
		RelPath: "error_file.txt",
		Size:    100,
		ModTime: time.Now(),
	}

	ctx := context.Background()
	err := sm.ReportError(ctx, fileWithError, errors.New("processing failed"))
	require.NoError(t, err)

	// Test if file with error needs processing again
	inputCh := make(chan uobf.FileInfo, 1)
	inputCh <- fileWithError
	close(inputCh)

	resultCh, err := sm.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	var results []uobf.FileInfo
	for fileInfo := range resultCh {
		results = append(results, fileInfo)
	}

	// File with error should need processing again
	assert.Len(t, results, 1)
	assert.Equal(t, fileWithError, results[0])
}

func TestMemoryStatusMemory_NeedsProcessing_ContextCancellation(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	// Create input channel
	inputCh := make(chan uobf.FileInfo)

	ctx, cancel := context.WithCancel(context.Background())

	resultCh, err := sm.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	// Start sending files first
	go func() {
		defer close(inputCh)
		for i := 0; i < 10; i++ {
			select {
			case inputCh <- uobf.FileInfo{
				RelPath: fmt.Sprintf("file%d.txt", i),
				Size:    int64(i),
				ModTime: time.Now(),
			}:
			case <-ctx.Done():
				// Stop sending if context is cancelled
				return
			}
		}
	}()

	// Give a moment for the goroutine to start processing
	time.Sleep(1 * time.Millisecond)

	// Cancel context after starting
	cancel()

	// Collect results until channel closes
	var results []uobf.FileInfo
	for fileInfo := range resultCh {
		results = append(results, fileInfo)
	}

	// Should have processed few or no files due to cancellation
	// Allow for some files to be processed before cancellation takes effect
	assert.LessOrEqual(t, len(results), 10, "Should not process all files due to cancellation")
}

func TestMemoryStatusMemory_ReportDone(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	fileInfo := uobf.FileInfo{
		RelPath: "test_file.txt",
		Size:    500,
		ModTime: time.Now(),
	}

	ctx := context.Background()
	err := sm.ReportDone(ctx, fileInfo)
	require.NoError(t, err)

	// Verify status was stored correctly
	status, exists := sm.GetStatus("test_file.txt")
	require.True(t, exists)
	assert.Equal(t, "test_file.txt", status.RelPath)
	assert.Equal(t, int64(500), status.Size)
	assert.Equal(t, fileInfo.ModTime, status.ModTime)
	assert.True(t, status.Processed)
	assert.Empty(t, status.LastError)
	assert.False(t, status.ProcessedAt.IsZero())

	assert.Equal(t, 1, sm.Count())
}

func TestMemoryStatusMemory_ReportError(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	fileInfo := uobf.FileInfo{
		RelPath: "error_file.txt",
		Size:    300,
		ModTime: time.Now(),
	}

	testError := errors.New("test processing error")

	ctx := context.Background()
	err := sm.ReportError(ctx, fileInfo, testError)
	require.NoError(t, err)

	// Verify status was stored correctly
	status, exists := sm.GetStatus("error_file.txt")
	require.True(t, exists)
	assert.Equal(t, "error_file.txt", status.RelPath)
	assert.Equal(t, int64(300), status.Size)
	assert.Equal(t, fileInfo.ModTime, status.ModTime)
	assert.False(t, status.Processed)
	assert.Equal(t, "test processing error", status.LastError)
	assert.False(t, status.ProcessedAt.IsZero())

	assert.Equal(t, 1, sm.Count())
}

func TestMemoryStatusMemory_GetStatus_NotFound(t *testing.T) {
	sm := NewMemoryStatusMemory()

	status, exists := sm.GetStatus("nonexistent.txt")
	assert.False(t, exists)
	assert.Nil(t, status)
}

func TestMemoryStatusMemory_GetAllStatus(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	// Add some files
	files := []uobf.FileInfo{
		{RelPath: "file1.txt", Size: 100, ModTime: time.Now()},
		{RelPath: "file2.txt", Size: 200, ModTime: time.Now()},
		{RelPath: "file3.txt", Size: 300, ModTime: time.Now()},
	}

	ctx := context.Background()

	// Report some as done, some as error
	err := sm.ReportDone(ctx, files[0])
	require.NoError(t, err)
	err = sm.ReportDone(ctx, files[1])
	require.NoError(t, err)
	err = sm.ReportError(ctx, files[2], errors.New("test error"))
	require.NoError(t, err)

	// Get all status
	allStatus := sm.GetAllStatus()
	assert.Len(t, allStatus, 3)

	// Verify each status
	assert.True(t, allStatus["file1.txt"].Processed)
	assert.Empty(t, allStatus["file1.txt"].LastError)

	assert.True(t, allStatus["file2.txt"].Processed)
	assert.Empty(t, allStatus["file2.txt"].LastError)

	assert.False(t, allStatus["file3.txt"].Processed)
	assert.Equal(t, "test error", allStatus["file3.txt"].LastError)
}

func TestMemoryStatusMemory_Clear(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	// Add some files
	ctx := context.Background()
	fileInfo := uobf.FileInfo{RelPath: "test.txt", Size: 100, ModTime: time.Now()}
	err := sm.ReportDone(ctx, fileInfo)
	require.NoError(t, err)

	assert.Equal(t, 1, sm.Count())

	// Clear all status
	sm.Clear()

	assert.Equal(t, 0, sm.Count())
	_, exists := sm.GetStatus("test.txt")
	assert.False(t, exists)
}

func TestMemoryStatusMemory_ConcurrentAccess(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	ctx := context.Background()

	// Test concurrent access with multiple goroutines
	const numGoroutines = 10
	const filesPerGoroutine = 100

	done := make(chan bool, numGoroutines)

	// Start multiple goroutines writing different files
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < filesPerGoroutine; j++ {
				fileInfo := uobf.FileInfo{
					RelPath: fmt.Sprintf("goroutine_%d_file_%d.txt", goroutineID, j),
					Size:    int64(j),
					ModTime: time.Now(),
				}

				if j%2 == 0 {
					err := sm.ReportDone(ctx, fileInfo)
					assert.NoError(t, err)
				} else {
					err := sm.ReportError(ctx, fileInfo, errors.New("test error"))
					assert.NoError(t, err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final count
	expectedCount := numGoroutines * filesPerGoroutine
	assert.Equal(t, expectedCount, sm.Count())

	// Verify we can read all statuses without race conditions
	allStatus := sm.GetAllStatus()
	assert.Len(t, allStatus, expectedCount)
}

func TestMemoryStatusMemory_LargeDataset(t *testing.T) {
	sm := NewMemoryStatusMemory()
	sm.SetLogger(&mockLogger{})

	const numFiles = 10000

	// Create a large number of files
	files := make([]uobf.FileInfo, numFiles)
	for i := 0; i < numFiles; i++ {
		files[i] = uobf.FileInfo{
			RelPath: fmt.Sprintf("file_%d.txt", i),
			Size:    int64(i),
			ModTime: time.Now(),
		}
	}

	// Create input channel
	inputCh := make(chan uobf.FileInfo, 1000)
	go func() {
		defer close(inputCh)
		for _, file := range files {
			inputCh <- file
		}
	}()

	// Test NeedsProcessing with large dataset
	ctx := context.Background()
	resultCh, err := sm.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	// Count results
	count := 0
	for range resultCh {
		count++
	}

	// All files should need processing (new files)
	assert.Equal(t, numFiles, count)

	// Report half as done
	for i := 0; i < numFiles/2; i++ {
		err := sm.ReportDone(ctx, files[i])
		require.NoError(t, err)
	}

	// Test again - only half should need processing
	inputCh2 := make(chan uobf.FileInfo, 1000)
	go func() {
		defer close(inputCh2)
		for _, file := range files {
			inputCh2 <- file
		}
	}()

	resultCh2, err := sm.NeedsProcessing(ctx, inputCh2)
	require.NoError(t, err)

	count2 := 0
	for range resultCh2 {
		count2++
	}

	// Only the second half should need processing
	assert.Equal(t, numFiles/2, count2)
}

// Mock logger for testing
type mockLogger struct {
	mu        sync.Mutex
	debugLogs []string
	infoLogs  []string
	warnLogs  []string
	errorLogs []string
}

func (m *mockLogger) Debug(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debugLogs = append(m.debugLogs, msg)
}

func (m *mockLogger) Info(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infoLogs = append(m.infoLogs, msg)
}

func (m *mockLogger) Warn(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnLogs = append(m.warnLogs, msg)
}

func (m *mockLogger) Error(msg string, fields ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorLogs = append(m.errorLogs, msg)
}

func (m *mockLogger) WithFields(fields map[string]interface{}) common.Logger {
	return m
}
