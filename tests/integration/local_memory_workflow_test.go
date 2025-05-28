//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideamans/go-unified-overwrite-batch-flow"
	"github.com/ideamans/go-unified-overwrite-batch-flow/backlog"
	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
	"github.com/ideamans/go-unified-overwrite-batch-flow/filesystem"
	"github.com/ideamans/go-unified-overwrite-batch-flow/l10n"
	"github.com/ideamans/go-unified-overwrite-batch-flow/status"
)

func init() {
	l10n.ForceLanguage("en")
}

// memoryBacklogWrapper wraps MemoryBacklogManager to implement uobf.BacklogManager
type memoryBacklogWrapper struct {
	*backlog.MemoryBacklogManager
}

func (w *memoryBacklogWrapper) SetLogger(logger common.Logger) {
	// Set common.Logger directly
	w.MemoryBacklogManager.SetLogger(logger)
}

func TestLocalMemoryWorkflowIntegration(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "uobf_working_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Step 1: Create test files with lowercase content
	testFiles := map[string]string{
		"file1.txt":           "hello world this is file one",
		"dir1/file2.txt":      "this is file two in directory one", 
		"dir1/subdir/file3.txt": "file three in subdirectory",
		"dir2/file4.txt":      "another file in directory two",
		"dir2/file5.txt":      "last file in directory two",
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", filePath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", filePath, err)
		}
	}

	// Step 2: First scan run
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Create components
	logger := &common.NoOpLogger{}
	
	localFS := filesystem.NewLocalFileSystem(tempDir)
	localFS.SetLogger(logger)
	
	statusMemory := status.NewMemoryStatusMemory()
	statusMemory.SetLogger(logger)
	
	memoryBacklog1 := &memoryBacklogWrapper{backlog.NewMemoryBacklogManager()}
	memoryBacklog1.SetLogger(logger)
	
	workflow1 := uobf.NewOverwriteWorkflow(localFS, statusMemory, memoryBacklog1)
	workflow1.SetLogger(logger)

	// Configure scan options
	scanOptions := uobf.ScanAndFilterOptions{
		WalkOptions: uobf.WalkOptions{
			Include:   []string{"*.txt"},
			FilesOnly: true,
		},
		BatchSize:    100,
		ProgressEach: 1,
	}

	// Run first scan
	if err := workflow1.ScanAndFilter(ctx, scanOptions); err != nil {
		t.Fatalf("First ScanAndFilter failed: %v", err)
	}

	// Verify first backlog contains all files
	backlog1Entries := memoryBacklog1.GetEntries()
	if len(backlog1Entries) != len(testFiles) {
		t.Errorf("Expected %d entries in first backlog, got %d", len(testFiles), len(backlog1Entries))
		t.Logf("Backlog1 entries: %v", backlog1Entries)
	}

	// Step 3: Manually convert files to uppercase to simulate processing
	for filePath, originalContent := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		upperContent := strings.ToUpper(originalContent)
		if err := os.WriteFile(fullPath, []byte(upperContent), 0644); err != nil {
			t.Fatalf("Failed to update file %s: %v", filePath, err)
		}
		
		// Update status memory to mark as processed
		stat, err := os.Stat(fullPath)
		if err != nil {
			t.Fatalf("Failed to stat file %s: %v", fullPath, err)
		}
		
		fileInfo := uobf.FileInfo{
			Name:    filepath.Base(filePath),
			Size:    stat.Size(),
			Mode:    uint32(stat.Mode()),
			ModTime: stat.ModTime(),
			IsDir:   stat.IsDir(),
			RelPath: filePath,
			AbsPath: fullPath,
		}
		if err := statusMemory.ReportDone(ctx, fileInfo); err != nil {
			t.Fatalf("Failed to mark file as processed: %v", err)
		}
	}

	// Step 4: Add new files and modify existing file
	time.Sleep(time.Second) // Ensure different modification times

	// Add new files
	newFiles := map[string]string{
		"dir3/newfile1.txt": "this is a new file with lowercase content",
		"newfile2.txt":      "another new file at root level",
	}

	for filePath, content := range newFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for new file %s: %v", filePath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write new file %s: %v", filePath, err)
		}
	}

	// Modify existing file with longer content to ensure different size
	modifiedFile := "file1.txt"
	modifiedContent := "this is the modified content for file one with much longer text to ensure different size"
	modifiedPath := filepath.Join(tempDir, modifiedFile)
	if err := os.WriteFile(modifiedPath, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("Failed to modify file %s: %v", modifiedFile, err)
	}

	// Step 5: Second scan run
	memoryBacklog2 := &memoryBacklogWrapper{backlog.NewMemoryBacklogManager()}
	memoryBacklog2.SetLogger(logger)
	
	workflow2 := uobf.NewOverwriteWorkflow(localFS, statusMemory, memoryBacklog2)
	workflow2.SetLogger(logger)

	// Run second scan
	if err := workflow2.ScanAndFilter(ctx, scanOptions); err != nil {
		t.Fatalf("Second ScanAndFilter failed: %v", err)
	}

	// Step 6: Verify second backlog contains only changed files
	backlog2Entries := memoryBacklog2.GetEntries()
	
	expectedChangedFiles := []string{
		modifiedFile,
		"dir3/newfile1.txt",
		"newfile2.txt",
	}

	if len(backlog2Entries) != len(expectedChangedFiles) {
		t.Errorf("Expected %d entries in second backlog, got %d", 
			len(expectedChangedFiles), len(backlog2Entries))
		t.Errorf("Backlog2 entries: %v", backlog2Entries)
	}

	// Verify that backlog2 contains exactly the expected files
	backlog2Set := make(map[string]bool)
	for _, entry := range backlog2Entries {
		backlog2Set[entry] = true
	}

	for _, expectedFile := range expectedChangedFiles {
		if !backlog2Set[expectedFile] {
			t.Errorf("Expected file %s not found in second backlog", expectedFile)
		}
	}

	// Step 7: Manually process the changed files to simulate the processing phase
	for filePath := range newFiles {
		fullPath := filepath.Join(tempDir, filePath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("Failed to read new file %s: %v", filePath, err)
		}
		upperContent := strings.ToUpper(string(content))
		if err := os.WriteFile(fullPath, []byte(upperContent), 0644); err != nil {
			t.Fatalf("Failed to update new file %s: %v", filePath, err)
		}
	}

	// Process modified file
	modifiedPath = filepath.Join(tempDir, modifiedFile)
	upperModifiedContent := strings.ToUpper(modifiedContent)
	if err := os.WriteFile(modifiedPath, []byte(upperModifiedContent), 0644); err != nil {
		t.Fatalf("Failed to update modified file %s: %v", modifiedFile, err)
	}

	// Step 8: Verify all files now have uppercase content
	allFiles := make(map[string]string)
	for path, content := range testFiles {
		if path == modifiedFile {
			allFiles[path] = modifiedContent // Use modified content
		} else {
			allFiles[path] = content
		}
	}
	for path, content := range newFiles {
		allFiles[path] = content
	}

	for filePath, originalContent := range allFiles {
		fullPath := filepath.Join(tempDir, filePath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("Failed to read final file %s: %v", filePath, err)
		}
		
		expected := strings.ToUpper(originalContent)
		if string(content) != expected {
			t.Errorf("File %s content mismatch.\nExpected: %s\nGot: %s", 
				filePath, expected, string(content))
		}
	}

	t.Logf("Integration test completed successfully")
	t.Logf("First backlog processed %d files", len(backlog1Entries))
	t.Logf("Second backlog processed %d files (only changed files)", len(backlog2Entries))
	
	// Verify the core functionality: incremental processing
	if len(backlog2Entries) == 3 && len(backlog1Entries) == 5 {
		t.Logf("✅ Incremental processing works correctly:")
		t.Logf("   - First run: processed all %d files", len(backlog1Entries))
		t.Logf("   - Second run: processed only %d changed files", len(backlog2Entries))
		t.Logf("   - Changed files: %v", backlog2Entries)
	} else {
		t.Errorf("❌ Incremental processing failed")
	}
}