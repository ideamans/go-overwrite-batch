package backlog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideamans/go-unified-overwrite-batch-flow/l10n"
)

func init() {
	// Force English for consistent test results
	l10n.ForceLanguage("en")
}

func TestMemoryBacklogManager_WriteAndRead(t *testing.T) {
	manager := NewMemoryBacklogManager()

	logger := &testLogger{}
	manager.SetLogger(logger)

	// Create test data
	testRelPaths := []string{
		"file1.txt",
		"dir/file2.txt",
		"subdir/file3.txt",
		"another/path/file4.txt",
	}

	// Test writing
	ctx := context.Background()
	relPathChan := make(chan string, len(testRelPaths))

	for _, relPath := range testRelPaths {
		relPathChan <- relPath
	}
	close(relPathChan)

	err := manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write backlog: %v", err)
	}

	// Verify entries were stored
	if manager.Size() != len(testRelPaths) {
		t.Errorf("Expected %d entries in backlog, got %d", len(testRelPaths), manager.Size())
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

	// Verify data integrity (order should be preserved)
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

func TestMemoryBacklogManager_EmptyBacklog(t *testing.T) {
	manager := NewMemoryBacklogManager()

	// Test counting empty backlog
	ctx := context.Background()
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

	// Test writing to empty backlog
	relPathChan := make(chan string)
	close(relPathChan) // Immediately close to simulate empty input

	err = manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed to write empty backlog: %v", err)
	}

	if manager.Size() != 0 {
		t.Errorf("Expected backlog to remain empty, got %d entries", manager.Size())
	}
}

func TestMemoryBacklogManager_ConcurrentWriteAndRead(t *testing.T) {
	manager := NewMemoryBacklogManager()

	testRelPaths := []string{
		"concurrent1.txt",
		"concurrent2.txt",
		"concurrent3.txt",
	}

	ctx := context.Background()

	// Test concurrent write and count operations
	relPathChan := make(chan string, len(testRelPaths))
	for _, relPath := range testRelPaths {
		relPathChan <- relPath
	}
	close(relPathChan)

	// Start writing in background
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- manager.StartWriting(ctx, relPathChan)
	}()

	// Wait for write to complete
	err := <-writeDone
	if err != nil {
		t.Fatalf("Failed to write backlog concurrently: %v", err)
	}

	// Test concurrent read operations
	readDone := make(chan error, 2)
	results := make([][]string, 2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			readChan, err := manager.StartReading(ctx)
			if err != nil {
				readDone <- err
				return
			}

			var paths []string
			for relPath := range readChan {
				paths = append(paths, relPath)
			}
			results[idx] = paths
			readDone <- nil
		}(i)
	}

	// Wait for both reads to complete
	for i := 0; i < 2; i++ {
		err := <-readDone
		if err != nil {
			t.Fatalf("Failed to read backlog concurrently: %v", err)
		}
	}

	// Verify both reads got the same data
	if len(results[0]) != len(results[1]) {
		t.Errorf("Concurrent reads returned different lengths: %d vs %d",
			len(results[0]), len(results[1]))
	}

	for i := 0; i < len(results[0]) && i < len(results[1]); i++ {
		if results[0][i] != results[1][i] {
			t.Errorf("Concurrent reads differed at index %d: %s vs %s",
				i, results[0][i], results[1][i])
		}
	}
}

func TestMemoryBacklogManager_ContextCancellation(t *testing.T) {
	manager := NewMemoryBacklogManager()

	// Test write cancellation
	ctx, cancel := context.WithCancel(context.Background())
	relPathChan := make(chan string, 1)
	relPathChan <- "test.txt"

	// Cancel context immediately
	cancel()

	err := manager.StartWriting(ctx, relPathChan)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	// Test read cancellation
	manager.SetEntries([]string{"file1.txt", "file2.txt"})

	cancelCtx, cancel2 := context.WithCancel(context.Background())
	readChan, err := manager.StartReading(cancelCtx)
	if err != nil {
		t.Fatalf("Failed to start reading: %v", err)
	}

	// Read one entry then cancel
	<-readChan
	cancel2()

	// Channel should close due to cancellation
	select {
	case _, ok := <-readChan:
		if ok {
			// Might get one more due to buffering, but should eventually close
			select {
			case _, stillOpen := <-readChan:
				if stillOpen {
					t.Error("Expected channel to close after cancellation")
				}
			case <-time.After(100 * time.Millisecond):
				// Timeout is acceptable
			}
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to read at least one entry before cancellation")
	}

	// Test count cancellation
	cancelCtx2, cancel3 := context.WithCancel(context.Background())
	cancel3() // Cancel immediately

	_, err = manager.CountRelPaths(cancelCtx2)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error for count, got: %v", err)
	}
}

func TestMemoryBacklogManager_LargeBacklog(t *testing.T) {
	manager := NewMemoryBacklogManager()

	// Create a large number of entries
	const numEntries = 50000
	relPathChan := make(chan string, 1000) // Buffer for performance

	ctx := context.Background()

	// Start writing in a goroutine
	go func() {
		defer close(relPathChan)
		for i := 0; i < numEntries; i++ {
			relPathChan <- fmt.Sprintf("file_%d.txt", i)
		}
	}()

	err := manager.StartWriting(ctx, relPathChan)
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

	// Test reading (just count, don't store all)
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

func TestMemoryBacklogManager_HelperMethods(t *testing.T) {
	manager := NewMemoryBacklogManager()

	// Test initial state
	if manager.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", manager.Size())
	}

	entries := manager.GetEntries()
	if len(entries) != 0 {
		t.Errorf("Expected empty entries, got %d", len(entries))
	}

	// Test SetEntries
	testEntries := []string{"file1.txt", "file2.txt", "file3.txt"}
	manager.SetEntries(testEntries)

	if manager.Size() != len(testEntries) {
		t.Errorf("Expected size %d after SetEntries, got %d", len(testEntries), manager.Size())
	}

	retrievedEntries := manager.GetEntries()
	if len(retrievedEntries) != len(testEntries) {
		t.Errorf("Expected %d entries from GetEntries, got %d", len(testEntries), len(retrievedEntries))
	}

	for i, expected := range testEntries {
		if i < len(retrievedEntries) && retrievedEntries[i] != expected {
			t.Errorf("Entry mismatch at index %d: expected %s, got %s",
				i, expected, retrievedEntries[i])
		}
	}

	// Test Clear
	manager.Clear()
	if manager.Size() != 0 {
		t.Errorf("Expected size 0 after Clear, got %d", manager.Size())
	}

	// Test String method
	stringRepr := manager.String()
	expectedString := "MemoryBacklogManager{entries: 0}"
	if stringRepr != expectedString {
		t.Errorf("Expected string representation %s, got %s", expectedString, stringRepr)
	}
}

func TestMemoryBacklogManager_OverwriteBehavior(t *testing.T) {
	manager := NewMemoryBacklogManager()

	// First write
	ctx := context.Background()
	firstPaths := []string{"first1.txt", "first2.txt"}

	relPathChan := make(chan string, len(firstPaths))
	for _, path := range firstPaths {
		relPathChan <- path
	}
	close(relPathChan)

	err := manager.StartWriting(ctx, relPathChan)
	if err != nil {
		t.Fatalf("Failed first write: %v", err)
	}

	if manager.Size() != len(firstPaths) {
		t.Errorf("Expected %d entries after first write, got %d", len(firstPaths), manager.Size())
	}

	// Second write (should overwrite)
	secondPaths := []string{"second1.txt", "second2.txt", "second3.txt"}

	relPathChan2 := make(chan string, len(secondPaths))
	for _, path := range secondPaths {
		relPathChan2 <- path
	}
	close(relPathChan2)

	err = manager.StartWriting(ctx, relPathChan2)
	if err != nil {
		t.Fatalf("Failed second write: %v", err)
	}

	if manager.Size() != len(secondPaths) {
		t.Errorf("Expected %d entries after second write, got %d", len(secondPaths), manager.Size())
	}

	// Verify only second paths are present
	entries := manager.GetEntries()
	for i, expected := range secondPaths {
		if i < len(entries) && entries[i] != expected {
			t.Errorf("Entry mismatch at index %d: expected %s, got %s",
				i, expected, entries[i])
		}
	}

	// Should not contain any first paths
	for _, firstPath := range firstPaths {
		for _, entry := range entries {
			if entry == firstPath {
				t.Errorf("Found first write entry %s after second write", firstPath)
			}
		}
	}
}
