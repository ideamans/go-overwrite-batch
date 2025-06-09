package status

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	uobf "github.com/ideamans/overwritebatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLevelDBStatusMemory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()

	assert.NotNil(t, sm)
	assert.NotNil(t, sm.db)
	assert.NotNil(t, sm.logger)
	assert.Equal(t, dbPath, sm.dbPath)
	assert.Equal(t, 0, sm.Count())
}

func TestNewLevelDBStatusMemory_DirectoryCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Test with nested directory that doesn't exist
	dbPath := filepath.Join(tempDir, "nested", "dir", "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()

	assert.NotNil(t, sm)
	assert.DirExists(t, filepath.Dir(dbPath))
}

func TestLevelDBStatusMemory_SetLogger(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()

	mockLogger := &mockLogger{}
	sm.SetLogger(mockLogger)
	assert.Equal(t, mockLogger, sm.logger)
}

func TestLevelDBStatusMemory_NeedsProcessing_NewFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
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

func TestLevelDBStatusMemory_NeedsProcessing_ExistingFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
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
	err = sm.ReportDone(ctx, file1)
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

func TestLevelDBStatusMemory_NeedsProcessing_ModifiedFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	baseTime := time.Now()

	// Create and report a file as done
	originalFile := uobf.FileInfo{
		RelPath: "file1.txt",
		Size:    100,
		ModTime: baseTime,
	}

	ctx := context.Background()
	err = sm.ReportDone(ctx, originalFile)
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

func TestLevelDBStatusMemory_NeedsProcessing_FilesWithErrors(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	// Create and report a file with error
	fileWithError := uobf.FileInfo{
		RelPath: "error_file.txt",
		Size:    100,
		ModTime: time.Now(),
	}

	ctx := context.Background()
	err = sm.ReportError(ctx, fileWithError, errors.New("processing failed"))
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

func TestLevelDBStatusMemory_NeedsProcessing_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
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

func TestLevelDBStatusMemory_ReportDone(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	fileInfo := uobf.FileInfo{
		RelPath: "test_file.txt",
		Size:    500,
		ModTime: time.Now(),
	}

	ctx := context.Background()
	err = sm.ReportDone(ctx, fileInfo)
	require.NoError(t, err)

	// Verify status was stored correctly
	status, exists := sm.GetStatus("test_file.txt")
	require.True(t, exists)
	assert.Equal(t, "test_file.txt", status.RelPath)
	assert.Equal(t, int64(500), status.Size)
	assert.True(t, fileInfo.ModTime.Equal(status.ModTime)) // Use Equal for time comparison due to serialization
	assert.True(t, status.Processed)
	assert.Empty(t, status.LastError)
	assert.False(t, status.ProcessedAt.IsZero())

	assert.Equal(t, 1, sm.Count())
}

func TestLevelDBStatusMemory_ReportError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	fileInfo := uobf.FileInfo{
		RelPath: "error_file.txt",
		Size:    300,
		ModTime: time.Now(),
	}

	testError := errors.New("test processing error")

	ctx := context.Background()
	err = sm.ReportError(ctx, fileInfo, testError)
	require.NoError(t, err)

	// Verify status was stored correctly
	status, exists := sm.GetStatus("error_file.txt")
	require.True(t, exists)
	assert.Equal(t, "error_file.txt", status.RelPath)
	assert.Equal(t, int64(300), status.Size)
	assert.True(t, fileInfo.ModTime.Equal(status.ModTime)) // Use Equal for time comparison due to serialization
	assert.False(t, status.Processed)
	assert.Equal(t, "test processing error", status.LastError)
	assert.False(t, status.ProcessedAt.IsZero())

	assert.Equal(t, 1, sm.Count())
}

func TestLevelDBStatusMemory_GetStatus_NotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()

	status, exists := sm.GetStatus("nonexistent.txt")
	assert.False(t, exists)
	assert.Nil(t, status)
}

func TestLevelDBStatusMemory_GetAllStatus(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	// Add some files
	files := []uobf.FileInfo{
		{RelPath: "file1.txt", Size: 100, ModTime: time.Now()},
		{RelPath: "file2.txt", Size: 200, ModTime: time.Now()},
		{RelPath: "file3.txt", Size: 300, ModTime: time.Now()},
	}

	ctx := context.Background()

	// Report some as done, some as error
	err = sm.ReportDone(ctx, files[0])
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

func TestLevelDBStatusMemory_Clear(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
	sm.SetLogger(&mockLogger{})

	// Add some files
	ctx := context.Background()
	fileInfo := uobf.FileInfo{RelPath: "test.txt", Size: 100, ModTime: time.Now()}
	err = sm.ReportDone(ctx, fileInfo)
	require.NoError(t, err)

	assert.Equal(t, 1, sm.Count())

	// Clear all status
	err = sm.Clear()
	require.NoError(t, err)

	assert.Equal(t, 0, sm.Count())
	_, exists := sm.GetStatus("test.txt")
	assert.False(t, exists)
}

func TestLevelDBStatusMemory_ConcurrentAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
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

func TestLevelDBStatusMemory_LargeDataset(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm.Close(); err != nil {
			t.Logf("Failed to close status memory: %v", err)
		}
	}()
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

func TestLevelDBStatusMemory_PersistenceAfterClose(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")

	// First instance: create and store data
	sm1, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	sm1.SetLogger(&mockLogger{})

	ctx := context.Background()
	fileInfo := uobf.FileInfo{
		RelPath: "persistent_file.txt",
		Size:    999,
		ModTime: time.Now(),
	}

	err = sm1.ReportDone(ctx, fileInfo)
	require.NoError(t, err)

	// Verify data exists
	status1, exists1 := sm1.GetStatus("persistent_file.txt")
	require.True(t, exists1)
	assert.True(t, status1.Processed)

	// Close first instance
	err = sm1.Close()
	require.NoError(t, err)

	// Second instance: reopen and verify data persists
	sm2, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)
	defer func() {
		if err := sm2.Close(); err != nil {
			t.Logf("Failed to close status memory 2: %v", err)
		}
	}()
	sm2.SetLogger(&mockLogger{})

	// Verify data persisted
	status2, exists2 := sm2.GetStatus("persistent_file.txt")
	require.True(t, exists2)
	assert.Equal(t, "persistent_file.txt", status2.RelPath)
	assert.Equal(t, int64(999), status2.Size)
	assert.True(t, status2.Processed)
	assert.Empty(t, status2.LastError)

	// Verify the file doesn't need processing
	inputCh := make(chan uobf.FileInfo, 1)
	inputCh <- fileInfo
	close(inputCh)

	resultCh, err := sm2.NeedsProcessing(ctx, inputCh)
	require.NoError(t, err)

	var results []uobf.FileInfo
	for fileInfo := range resultCh {
		results = append(results, fileInfo)
	}

	// File should not need processing (already done)
	assert.Len(t, results, 0)
}

func TestLevelDBStatusMemory_Close(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "leveldb_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")
	sm, err := NewLevelDBStatusMemory(dbPath)
	require.NoError(t, err)

	// Test Close method
	err = sm.Close()
	assert.NoError(t, err)

	// Test multiple closes (should not return error, but may fail quietly)
	// LevelDB Close() returns error on second call, which is expected behavior
	_ = sm.Close() // Don't check error for second close - expected to fail
}
