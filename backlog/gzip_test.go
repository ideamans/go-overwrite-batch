package backlog

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideamans/go-unified-overwright-batch-flow/l10n"
)

// testFileInfo implements FileInfo interface for testing
type testFileInfo struct {
	relPath string
	absPath string
	size    int64
	modTime int64
}

func (t *testFileInfo) GetRelPath() string {
	return t.relPath
}

func (t *testFileInfo) GetAbsPath() string {
	return t.absPath
}

func (t *testFileInfo) GetSize() int64 {
	return t.size
}

func (t *testFileInfo) GetModTime() int64 {
	return t.modTime
}

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
	testFiles := []FileInfo{
		&testFileInfo{
			relPath: "file1.txt",
			absPath: "/root/file1.txt",
			size:    1024,
			modTime: time.Now().Unix(),
		},
		&testFileInfo{
			relPath: "dir/file2.txt",
			absPath: "/root/dir/file2.txt",
			size:    2048,
			modTime: time.Now().Unix(),
		},
		&testFileInfo{
			relPath: "file3.txt",
			absPath: "/root/file3.txt",
			size:    512,
			modTime: time.Now().Unix(),
		},
	}

	// Test writing
	ctx := context.Background()
	entryChan := make(chan FileInfo, len(testFiles))
	
	for _, file := range testFiles {
		entryChan <- file
	}
	close(entryChan)

	err = manager.StartWriting(ctx, entryChan)
	if err != nil {
		t.Fatalf("Failed to write backlog: %v", err)
	}

	// Check if backlog file exists
	if _, err := os.Stat(backlogPath); os.IsNotExist(err) {
		t.Fatalf("Backlog file was not created: %s", backlogPath)
	}

	// Test counting
	count, err := manager.CountEntries(ctx)
	if err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}
	if count != int64(len(testFiles)) {
		t.Errorf("Expected %d entries, got %d", len(testFiles), count)
	}

	// Test reading
	readChan, err := manager.StartReading(ctx)
	if err != nil {
		t.Fatalf("Failed to start reading: %v", err)
	}

	var readFiles []FileInfo
	for entry := range readChan {
		readFiles = append(readFiles, entry)
	}

	if len(readFiles) != len(testFiles) {
		t.Errorf("Expected %d files, got %d", len(testFiles), len(readFiles))
	}

	// Verify data integrity
	for i, original := range testFiles {
		if i >= len(readFiles) {
			break
		}
		read := readFiles[i]
		
		if read.GetRelPath() != original.GetRelPath() {
			t.Errorf("RelPath mismatch at index %d: expected %s, got %s", 
				i, original.GetRelPath(), read.GetRelPath())
		}
		
		if read.GetAbsPath() != original.GetAbsPath() {
			t.Errorf("AbsPath mismatch at index %d: expected %s, got %s", 
				i, original.GetAbsPath(), read.GetAbsPath())
		}
		
		if read.GetSize() != original.GetSize() {
			t.Errorf("Size mismatch at index %d: expected %d, got %d", 
				i, original.GetSize(), read.GetSize())
		}
		
		if read.GetModTime() != original.GetModTime() {
			t.Errorf("ModTime mismatch at index %d: expected %d, got %d", 
				i, original.GetModTime(), read.GetModTime())
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
	entryChan := make(chan FileInfo)
	close(entryChan) // Immediately close to simulate empty input

	err = manager.StartWriting(ctx, entryChan)
	if err != nil {
		t.Fatalf("Failed to write empty backlog: %v", err)
	}

	// Test counting empty backlog
	count, err := manager.CountEntries(ctx)
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

	var readFiles []FileInfo
	for entry := range readChan {
		readFiles = append(readFiles, entry)
	}

	if len(readFiles) != 0 {
		t.Errorf("Expected 0 files from empty backlog, got %d", len(readFiles))
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
	_, err = manager.CountEntries(ctx)
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
	entryChan := make(chan FileInfo, 1)
	entryChan <- &testFileInfo{
		relPath: "file1.txt",
		absPath: "/root/file1.txt",
		size:    1024,
		modTime: time.Now().Unix(),
	}
	
	// Cancel context immediately
	cancel()
	
	// Writing should return context cancellation error
	err = manager.StartWriting(ctx, entryChan)
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
	entryChan := make(chan FileInfo, 2)
	entryChan <- &testFileInfo{relPath: "file1.txt", absPath: "/root/file1.txt", size: 1024, modTime: time.Now().Unix()}
	entryChan <- &testFileInfo{relPath: "file2.txt", absPath: "/root/file2.txt", size: 2048, modTime: time.Now().Unix()}
	close(entryChan)

	err = manager.StartWriting(ctx, entryChan)
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
	entryChan := make(chan FileInfo, 1000) // Buffer for performance

	ctx := context.Background()

	// Start writing in a goroutine
	go func() {
		defer close(entryChan)
		for i := 0; i < numEntries; i++ {
			entryChan <- &testFileInfo{
				relPath: filepath.Join("dir", "file"+string(rune(i%26+'a')), "test.txt"),
				absPath: filepath.Join("/root", "dir", "file"+string(rune(i%26+'a')), "test.txt"),
				size:    int64(i * 100),
				modTime: time.Now().Unix() + int64(i),
			}
		}
	}()

	err = manager.StartWriting(ctx, entryChan)
	if err != nil {
		t.Fatalf("Failed to write large backlog: %v", err)
	}

	// Test counting
	count, err := manager.CountEntries(ctx)
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