//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uobf "github.com/ideamans/go-unified-overwrite-batch-flow"
	"github.com/ideamans/go-unified-overwrite-batch-flow/backlog"
	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
	"github.com/ideamans/go-unified-overwrite-batch-flow/filesystem"
	"github.com/ideamans/go-l10n"
	"github.com/ideamans/go-unified-overwrite-batch-flow/status"
)

func init() {
	l10n.ForceLanguage("en")
}

// uppercaseProcessFunc converts file content to uppercase
func uppercaseProcessFunc(ctx context.Context, localPath string) (string, error) {
	content, err := os.ReadFile(localPath)
	if err != nil {
		return "", err
	}

	upperContent := strings.ToUpper(string(content))
	if err := os.WriteFile(localPath, []byte(upperContent), 0644); err != nil {
		return "", err
	}

	return localPath, nil
}

func TestLocalMemoryWorkflowIntegration(t *testing.T) {
	// Configurable number of generated files for testing with larger datasets
	const numGeneratedFiles = 1000

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "uobf_working_test_")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Step 1: Create test files with lowercase content
	testFiles := map[string]string{
		"file1.txt":             "hello world this is file one",
		"dir1/file2.txt":        "this is file two in directory one",
		"dir1/subdir/file3.txt": "file three in subdirectory",
		"dir2/file4.txt":        "another file in directory two",
		"dir2/file5.txt":        "last file in directory two",
	}

	// Create the original test files
	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", filePath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", filePath, err)
		}
	}

	// Create generated directory and many test files
	generatedDir := filepath.Join(tempDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		t.Fatalf("Failed to create generated directory: %v", err)
	}

	t.Logf("Creating %d generated test files...", numGeneratedFiles)
	for i := 1; i <= numGeneratedFiles; i++ {
		fileName := fmt.Sprintf("%d.txt", i)
		filePath := filepath.Join(generatedDir, fileName)
		content := fmt.Sprintf("test%d", i)

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write generated file %s: %v", fileName, err)
		}

		// Add to testFiles map for later tracking
		relPath := fmt.Sprintf("generated/%s", fileName)
		testFiles[relPath] = content
	}
	t.Logf("Created %d generated test files successfully", numGeneratedFiles)

	// Step 2: First scan run
	// Increase timeout for larger file count
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Create components
	logger := &common.NoOpLogger{}

	localFS := filesystem.NewLocalFileSystem(tempDir)
	localFS.SetLogger(logger)

	statusMemory := status.NewMemoryStatusMemory()
	statusMemory.SetLogger(logger)

	// Use GzipBacklogManager for more realistic testing
	backlog1Path := filepath.Join(tempDir, "backlog1.txt.gz")
	gzipBacklog1 := backlog.NewGzipBacklogManager(backlog1Path)
	gzipBacklog1.SetLogger(logger)

	workflow1 := uobf.NewOverwriteWorkflow(localFS, statusMemory, gzipBacklog1)
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

	// Verify first backlog contains all files by counting entries
	backlog1Count, err := gzipBacklog1.CountRelPaths(ctx)
	if err != nil {
		t.Fatalf("Failed to count backlog1 entries: %v", err)
	}
	if int(backlog1Count) != len(testFiles) {
		t.Errorf("Expected %d entries in first backlog, got %d", len(testFiles), int(backlog1Count))
	}

	// Step 3: Process files with concurrency 2 using ProcessFiles
	processOptions1 := uobf.ProcessingOptions{
		Concurrency:  4,
		RetryCount:   3,
		RetryDelay:   100 * time.Millisecond,
		ProgressEach: 1,
		ProcessFunc:  uppercaseProcessFunc,
	}

	if err := workflow1.ProcessFiles(ctx, processOptions1); err != nil {
		t.Fatalf("First ProcessFiles failed: %v", err)
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
	backlog2Path := filepath.Join(tempDir, "backlog2.txt.gz")
	gzipBacklog2 := backlog.NewGzipBacklogManager(backlog2Path)
	gzipBacklog2.SetLogger(logger)

	workflow2 := uobf.NewOverwriteWorkflow(localFS, statusMemory, gzipBacklog2)
	workflow2.SetLogger(logger)

	// Run second scan
	if err := workflow2.ScanAndFilter(ctx, scanOptions); err != nil {
		t.Fatalf("Second ScanAndFilter failed: %v", err)
	}

	// Step 6: Verify second backlog contains only changed files
	backlog2Count, err := gzipBacklog2.CountRelPaths(ctx)
	if err != nil {
		t.Fatalf("Failed to count backlog2 entries: %v", err)
	}

	expectedChangedFiles := []string{
		modifiedFile,
		"dir3/newfile1.txt",
		"newfile2.txt",
	}

	if int(backlog2Count) != len(expectedChangedFiles) {
		t.Errorf("Expected %d entries in second backlog, got %d",
			len(expectedChangedFiles), int(backlog2Count))
	}

	// Verify that backlog2 contains exactly the expected files
	backlog2Ch, err := gzipBacklog2.StartReading(ctx)
	if err != nil {
		t.Fatalf("Failed to read backlog2: %v", err)
	}

	backlog2Entries := make([]string, 0)
	for entry := range backlog2Ch {
		backlog2Entries = append(backlog2Entries, entry)
	}

	backlog2Set := make(map[string]bool)
	for _, entry := range backlog2Entries {
		backlog2Set[entry] = true
	}

	for _, expectedFile := range expectedChangedFiles {
		if !backlog2Set[expectedFile] {
			t.Errorf("Expected file %s not found in second backlog", expectedFile)
		}
	}

	// Step 7: Process changed files with concurrency 1 using ProcessFiles
	processOptions2 := uobf.ProcessingOptions{
		Concurrency:  1,
		RetryCount:   3,
		RetryDelay:   100 * time.Millisecond,
		ProgressEach: 1,
		ProcessFunc:  uppercaseProcessFunc,
	}

	if err := workflow2.ProcessFiles(ctx, processOptions2); err != nil {
		t.Fatalf("Second ProcessFiles failed: %v", err)
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
	t.Logf("First backlog processed %d files", backlog1Count)
	t.Logf("Second backlog processed %d files (only changed files)", backlog2Count)

	// Verify the core functionality: incremental processing
	expectedFirstRunCount := int64(5 + numGeneratedFiles) // 5 original + generated files
	expectedSecondRunCount := int64(3)                    // 1 modified + 2 new files

	if backlog2Count == expectedSecondRunCount && backlog1Count == expectedFirstRunCount {
		t.Logf("✅ Incremental processing works correctly:")
		t.Logf("   - First run: processed all %d files", backlog1Count)
		t.Logf("   - Second run: processed only %d changed files", backlog2Count)
		t.Logf("   - Changed files: %v", backlog2Entries)
	} else {
		t.Errorf("❌ Incremental processing failed: expected first run=%d, second run=%d, but got first run=%d, second run=%d",
			expectedFirstRunCount, expectedSecondRunCount, backlog1Count, backlog2Count)
	}
}
