package backlog

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
	"github.com/ideamans/go-l10n"
)

// testLogger implements Logger interface for testing
type testLogger struct {
	messages []string
}

func (t *testLogger) Info(msg string, fields ...interface{}) {
	t.messages = append(t.messages, "INFO: "+msg)
}

func (t *testLogger) Error(msg string, fields ...interface{}) {
	t.messages = append(t.messages, "ERROR: "+msg)
}

func (t *testLogger) Debug(msg string, fields ...interface{}) {
	t.messages = append(t.messages, "DEBUG: "+msg)
}

func (t *testLogger) Warn(msg string, fields ...interface{}) {
	t.messages = append(t.messages, "WARN: "+msg)
}

func (t *testLogger) WithFields(fields map[string]interface{}) common.Logger {
	return t
}

func init() {
	// Force English for consistent test results
	l10n.ForceLanguage("en")
}

func TestGzipBacklogManager_WriteAndRead(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "backlog_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backlogPath := filepath.Join(tempDir, "test.backlog.gz")
	manager := NewGzipBacklogManager(backlogPath)

	logger := &testLogger{}
	manager.SetLogger(logger)

	// Create test data
	testRelPaths := []string{
		"file1.txt",
		"dir/file2.txt",
		"file3.txt",
	}

	// Test writing
	ctx := context.Background()
	relPathChan := make(chan string, len(testRelPaths))

	for _, relPath := range testRelPaths {
		relPathChan <- relPath
	}
	close(relPathChan)

	err = manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write backlog: %v", err)
	}

	// Check if backlog file exists
	if _, err := os.Stat(backlogPath); os.IsNotExist(err) {
		t.Fatalf("Backlog file was not created: %s", backlogPath)
	}

	// Test counting
	count, err := manager.CountRelPaths(ctx)
	if err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}
	if count != int64(len(testRelPaths)) {
		t.Errorf("Expected %d entries, got %d", len(testRelPaths), count)
	}

	// Test reading
	readChan, err := manager.StartReading(ctx)
	if err != nil {
		t.Fatalf("Failed to start reading: %v", err)
	}

	var readRelPaths []string
	for relPath := range readChan {
		readRelPaths = append(readRelPaths, relPath)
	}

	if len(readRelPaths) != len(testRelPaths) {
		t.Errorf("Expected %d relative paths, got %d", len(testRelPaths), len(readRelPaths))
	}

	// Verify data integrity
	for i, original := range testRelPaths {
		if i >= len(readRelPaths) {
			break
		}
		read := readRelPaths[i]

		if read != original {
			t.Errorf("RelPath mismatch at index %d: expected %s, got %s",
				i, original, read)
		}
	}
}

func TestGzipBacklogManager_EmptyBacklog(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "backlog_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backlogPath := filepath.Join(tempDir, "empty.backlog.gz")
	manager := NewGzipBacklogManager(backlogPath)

	// Test writing empty backlog
	ctx := context.Background()
	relPathChan := make(chan string)
	close(relPathChan) // Immediately close to simulate empty input

	err = manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write empty backlog: %v", err)
	}

	// Test counting empty backlog
	count, err := manager.CountRelPaths(ctx)
	if err != nil {
		t.Fatalf("Failed to count empty backlog: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 entries in empty backlog, got %d", count)
	}

	// Test reading empty backlog
	readChan, err := manager.StartReading(ctx)
	if err != nil {
		t.Fatalf("Failed to read empty backlog: %v", err)
	}

	var readRelPaths []string
	for relPath := range readChan {
		readRelPaths = append(readRelPaths, relPath)
	}

	if len(readRelPaths) != 0 {
		t.Errorf("Expected 0 relative paths from empty backlog, got %d", len(readRelPaths))
	}
}

func TestGzipBacklogManager_NonExistentFile(t *testing.T) {
	manager := NewGzipBacklogManager("/nonexistent/path/backlog.gz")
	ctx := context.Background()

	// Test reading non-existent file
	_, err := manager.StartReading(ctx)
	if err == nil {
		t.Error("Expected error when reading non-existent file")
	}

	// Test counting non-existent file
	_, err = manager.CountRelPaths(ctx)
	if err == nil {
		t.Error("Expected error when counting non-existent file")
	}
}

func TestGzipBacklogManager_CancellationDuringWrite(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "backlog_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backlogPath := filepath.Join(tempDir, "cancelled.backlog.gz")
	manager := NewGzipBacklogManager(backlogPath)

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Create channel with some data
	relPathChan := make(chan string, 1)
	relPathChan <- "file1.txt"

	// Cancel context immediately
	cancel()

	// Writing should return context cancellation error
	err = manager.StartWriting(ctx, relPathChan)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

func TestGzipBacklogManager_CancellationDuringRead(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "backlog_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backlogPath := filepath.Join(tempDir, "read_cancel.backlog.gz")
	manager := NewGzipBacklogManager(backlogPath)

	// First write some data
	ctx := context.Background()
	relPathChan := make(chan string, 2)
	relPathChan <- "file1.txt"
	relPathChan <- "file2.txt"
	close(relPathChan)

	err = manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	// Now test cancellation during read
	cancelCtx, cancel := context.WithCancel(context.Background())
	readChan, err := manager.StartReading(cancelCtx)
	if err != nil {
		t.Fatalf("Failed to start reading: %v", err)
	}

	// Read one entry then cancel
	<-readChan
	cancel()

	// Try to read more - channel should close due to cancellation
	select {
	case _, ok := <-readChan:
		if ok {
			// We might get one more entry due to buffering, but eventually channel should close
			select {
			case _, stillOpen := <-readChan:
				if stillOpen {
					t.Error("Expected channel to close after cancellation")
				}
			case <-time.After(100 * time.Millisecond):
				// Timeout is acceptable - cancellation might take a moment
			}
		}
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable - cancellation might take a moment
	}
}

func TestGzipBacklogManager_LargeBacklog(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "backlog_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backlogPath := filepath.Join(tempDir, "large.backlog.gz")
	manager := NewGzipBacklogManager(backlogPath)

	// Create a large number of entries
	const numEntries = 50000
	relPathChan := make(chan string, 1000) // Buffer for performance

	ctx := context.Background()

	// Start writing in a goroutine
	go func() {
		defer close(relPathChan)
		for i := 0; i < numEntries; i++ {
			relPath := filepath.Join("dir", "file"+string(rune(i%26+'a')), "test.txt")
			relPathChan <- relPath
		}
	}()

	err = manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write large backlog: %v", err)
	}

	// Test counting
	count, err := manager.CountRelPaths(ctx)
	if err != nil {
		t.Fatalf("Failed to count large backlog: %v", err)
	}
	if count != numEntries {
		t.Errorf("Expected %d entries, got %d", numEntries, count)
	}

	// Test reading
	readChan, err := manager.StartReading(ctx)
	if err != nil {
		t.Fatalf("Failed to start reading large backlog: %v", err)
	}

	readCount := 0
	for range readChan {
		readCount++
	}

	if readCount != numEntries {
		t.Errorf("Expected to read %d entries, got %d", numEntries, readCount)
	}
}
