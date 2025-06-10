package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	uobf "github.com/ideamans/go-overwrite-batch"
	"github.com/ideamans/go-overwrite-batch/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocalFileSystem(t *testing.T) {
	fs := NewLocalFileSystem("/test/path")
	assert.NotNil(t, fs)
	assert.NotNil(t, fs.logger)
	assert.Equal(t, filepath.Clean("/test/path"), fs.rootPath)
}

func TestLocalFileSystem_Close(t *testing.T) {
	fs := NewLocalFileSystem("/test/path")
	err := fs.Close()
	assert.NoError(t, err)
}

func TestLocalFileSystem_SetLogger(t *testing.T) {
	fs := NewLocalFileSystem("/test/path")
	mockLogger := &mockLogger{}
	fs.SetLogger(mockLogger)
	assert.Equal(t, mockLogger, fs.logger)
}

func TestLocalFileSystem_Walk(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test files and directories
	createTestFiles(t, tempDir)

	tests := []struct {
		name          string
		rootPath      string
		options       uobf.WalkOptions
		expectedFiles []string
		wantErr       bool
	}{
		{
			name:     "walk all files",
			rootPath: tempDir,
			options:  uobf.WalkOptions{},
			expectedFiles: []string{
				"dir1",
				"dir1/file1.txt",
				"dir1/file2.log",
				"dir1/subdir",
				"dir1/subdir/file3.txt",
				"file0.txt",
			},
			wantErr: false,
		},
		{
			name:     "walk files only",
			rootPath: tempDir,
			options:  uobf.WalkOptions{FilesOnly: true},
			expectedFiles: []string{
				"dir1/file1.txt",
				"dir1/file2.log",
				"dir1/subdir/file3.txt",
				"file0.txt",
			},
			wantErr: false,
		},
		{
			name:     "walk with max depth 1",
			rootPath: tempDir,
			options:  uobf.WalkOptions{MaxDepth: 1},
			expectedFiles: []string{
				"dir1",
				"dir1/file1.txt",
				"dir1/file2.log",
				"dir1/subdir",
				"file0.txt",
			},
			wantErr: false,
		},
		{
			name:     "walk with include pattern",
			rootPath: tempDir,
			options:  uobf.WalkOptions{Include: []string{"*.txt"}},
			expectedFiles: []string{
				"dir1/file1.txt",
				"dir1/subdir/file3.txt",
				"file0.txt",
			},
			wantErr: false,
		},
		{
			name:     "walk with exclude pattern",
			rootPath: tempDir,
			options:  uobf.WalkOptions{Exclude: []string{"*.log"}},
			expectedFiles: []string{
				"dir1",
				"dir1/file1.txt",
				"dir1/subdir",
				"dir1/subdir/file3.txt",
				"file0.txt",
			},
			wantErr: false,
		},
		{
			name:     "non-existent root path",
			rootPath: "/non/existent/path",
			options:  uobf.WalkOptions{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewLocalFileSystem(tt.rootPath)
			fs.SetLogger(&mockLogger{})

			ch := make(chan uobf.FileInfo, 100)
			ctx := context.Background()

			// Run Walk in goroutine
			done := make(chan error, 1)
			go func() {
				defer close(ch)
				done <- fs.Walk(ctx, tt.options, ch)
			}()

			// Collect results
			var results []string
			for fileInfo := range ch {
				results = append(results, fileInfo.RelPath)
			}

			err := <-done

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedFiles, results)
			}
		})
	}
}

func TestLocalFileSystem_Walk_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	createTestFiles(t, tempDir)

	fs := NewLocalFileSystem(tempDir)
	ch := make(chan uobf.FileInfo, 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = fs.Walk(ctx, uobf.WalkOptions{}, ch)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestLocalFileSystem_Overwrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	content := "test content"
	err = os.WriteFile(srcFile, []byte(content), 0644)
	require.NoError(t, err)

	tests := []struct {
		name          string
		remoteRelPath string
		processFunc   func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error)
		wantErr       bool
		wantContent   string
		wantSkip      bool
	}{
		{
			name:          "successful overwrite",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Process the file (uppercase content)
				processedPath := srcFilePath + ".processed"
				processedContent := "PROCESSED: " + content
				err := os.WriteFile(processedPath, []byte(processedContent), 0644)
				if err != nil {
					return "", false, err
				}
				// Return true for autoRemove since we created a new file
				return processedPath, true, nil
			},
			wantErr:     false,
			wantContent: "PROCESSED: test content",
			wantSkip:    false,
		},
		{
			name:          "intentional skip",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Return empty string to skip upload
				return "", false, nil
			},
			wantErr:     false,
			wantContent: content, // Original content unchanged
			wantSkip:    true,
		},
		{
			name:          "processing error",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				return "", false, errors.New("processing failed")
			},
			wantErr:     true,
			wantContent: content, // Original content unchanged
			wantSkip:    false,
		},
		{
			name:          "non-existent source file",
			remoteRelPath: "nonexistent.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				return srcFilePath, false, nil
			},
			wantErr:  true,
			wantSkip: false,
		},
		{
			name:          "successful overwrite with autoRemove",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Process the file and request auto-removal
				processedPath := srcFilePath + ".processed_autoremove"
				processedContent := "AUTOREMOVE: " + content
				err := os.WriteFile(processedPath, []byte(processedContent), 0644)
				if err != nil {
					return "", false, err
				}
				// Return true for autoRemove
				return processedPath, true, nil
			},
			wantErr:     false,
			wantContent: "AUTOREMOVE: test content",
			wantSkip:    false,
		},
		{
			name:          "in-place modification (same file path)",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Modify the file in-place and return the same path
				inPlaceContent := "IN-PLACE: " + content
				err := os.WriteFile(srcFilePath, []byte(inPlaceContent), 0644)
				if err != nil {
					return "", false, err
				}
				// Return same path - file should NOT be deleted even with autoRemove=true
				return srcFilePath, true, nil
			},
			wantErr:     false,
			wantContent: "IN-PLACE: test content",
			wantSkip:    false,
		},
		{
			name:          "autoRemove false with different path",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Process the file but don't request auto-removal
				processedPath := srcFilePath + ".no_autoremove"
				processedContent := "NO_AUTOREMOVE: " + content
				err := os.WriteFile(processedPath, []byte(processedContent), 0644)
				if err != nil {
					return "", false, err
				}
				// Return false for autoRemove - file should remain
				return processedPath, false, nil
			},
			wantErr:     false,
			wantContent: "NO_AUTOREMOVE: test content",
			wantSkip:    false,
		},
		{
			name:          "non-existent processed file",
			remoteRelPath: "source.txt",
			processFunc: func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
				// Return a path that doesn't exist
				return "/tmp/non-existent-file-12345.txt", false, nil
			},
			wantErr:     true,
			wantContent: content, // Original content unchanged
			wantSkip:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh subdirectory for each test to avoid interference
			testSubDir := filepath.Join(tempDir, tt.name)
			err := os.MkdirAll(testSubDir, 0755)
			require.NoError(t, err)

			// Create the source file in the subdirectory if it should exist
			if tt.remoteRelPath != "nonexistent.txt" {
				testSrcFile := filepath.Join(testSubDir, tt.remoteRelPath)
				testSrcDir := filepath.Dir(testSrcFile)
				if testSrcDir != testSubDir {
					err = os.MkdirAll(testSrcDir, 0755)
					require.NoError(t, err)
				}
				err = os.WriteFile(testSrcFile, []byte(content), 0644)
				require.NoError(t, err)
			}

			fs := NewLocalFileSystem(testSubDir)
			fs.SetLogger(&mockLogger{})

			fileInfo, err := fs.Overwrite(context.Background(), tt.remoteRelPath, tt.processFunc)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fileInfo)
				assert.Equal(t, tt.remoteRelPath, fileInfo.RelPath)

				// Verify file content
				actualContent, err := os.ReadFile(filepath.Join(testSubDir, tt.remoteRelPath))
				assert.NoError(t, err)
				assert.Equal(t, tt.wantContent, string(actualContent))

				// Verify autoRemove behavior
				switch tt.name {
				case "successful overwrite with autoRemove":
					// The processed file should have been deleted
					processedFiles, err := filepath.Glob(filepath.Join(testSubDir, "*.processed_autoremove"))
					assert.NoError(t, err)
					assert.Empty(t, processedFiles, "Processed file should have been auto-removed")

				case "in-place modification (same file path)":
					// The temp file should still exist (not deleted because it's the same as srcFilePath)
					// This is handled by the temporary file cleanup in Overwrite method

				case "autoRemove false with different path":
					// The processed file should still exist
					// Note: The processed file is created in the system temp directory, not testSubDir
					processedFiles, err := filepath.Glob(filepath.Join(os.TempDir(), "uobf-download-*.no_autoremove"))
					assert.NoError(t, err)
					assert.GreaterOrEqual(t, len(processedFiles), 1, "Processed file should NOT have been removed when autoRemove=false")
					// Clean it up
					for _, f := range processedFiles {
						os.Remove(f)
					}
				}
			}
		})
	}
}

func TestLocalFileSystem_Overwrite_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create a larger source file to ensure context cancellation can happen during copy
	srcFile := filepath.Join(tempDir, "source.txt")
	// Create 10MB file to increase chance of catching cancellation
	largeContent := make([]byte, 10*1024*1024)
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	err = os.WriteFile(srcFile, largeContent, 0644)
	require.NoError(t, err)

	fs := NewLocalFileSystem(tempDir)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a short delay to allow copy to start
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err = fs.Overwrite(ctx, "source.txt", func(fileInfo uobf.FileInfo, srcFilePath string) (string, bool, error) {
		// Just return the same file path to simulate upload
		return srcFilePath, false, nil
	})

	// Either context canceled or successful completion is acceptable
	// on fast systems, the operation might complete before cancellation
	if err != nil {
		assert.Contains(t, err.Error(), "context canceled")
	} else {
		// If no error, file should still exist and be unchanged
		info, statErr := os.Stat(srcFile)
		assert.NoError(t, statErr)
		assert.Equal(t, int64(len(largeContent)), info.Size())
	}
}

// TestLocalFileSystem_Upload has been removed as Upload method no longer exists.
// Upload functionality is now part of the Overwrite method and is tested in TestLocalFileSystem_Overwrite.

// TestLocalFileSystem_Upload_ContextCancellation has been removed as Upload method no longer exists.
// Context cancellation during upload is now tested as part of TestLocalFileSystem_Overwrite_ContextCancellation.

func TestLocalFileSystem_shouldIncludeFile(t *testing.T) {
	fs := NewLocalFileSystem("/test/path")

	tests := []struct {
		name     string
		fileInfo uobf.FileInfo
		options  uobf.WalkOptions
		expected bool
	}{
		{
			name:     "no filters - include all",
			fileInfo: uobf.FileInfo{RelPath: "test.txt"},
			options:  uobf.WalkOptions{},
			expected: true,
		},
		{
			name:     "include pattern matches",
			fileInfo: uobf.FileInfo{RelPath: "test.txt"},
			options:  uobf.WalkOptions{Include: []string{"*.txt"}},
			expected: true,
		},
		{
			name:     "include pattern doesn't match",
			fileInfo: uobf.FileInfo{RelPath: "test.log"},
			options:  uobf.WalkOptions{Include: []string{"*.txt"}},
			expected: false,
		},
		{
			name:     "exclude pattern matches",
			fileInfo: uobf.FileInfo{RelPath: "test.log"},
			options:  uobf.WalkOptions{Exclude: []string{"*.log"}},
			expected: false,
		},
		{
			name:     "exclude pattern doesn't match",
			fileInfo: uobf.FileInfo{RelPath: "test.txt"},
			options:  uobf.WalkOptions{Exclude: []string{"*.log"}},
			expected: true,
		},
		{
			name:     "include and exclude - include wins",
			fileInfo: uobf.FileInfo{RelPath: "test.txt"},
			options:  uobf.WalkOptions{Include: []string{"*.txt"}, Exclude: []string{"test.*"}},
			expected: false, // exclude takes precedence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fs.shouldIncludeFile(tt.fileInfo, tt.options)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocalFileSystem_matchPattern(t *testing.T) {
	fs := NewLocalFileSystem("/test/path")

	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		{
			name:     "exact match",
			path:     "test.txt",
			pattern:  "test.txt",
			expected: true,
		},
		{
			name:     "wildcard match",
			path:     "test.txt",
			pattern:  "*.txt",
			expected: true,
		},
		{
			name:     "wildcard no match",
			path:     "test.log",
			pattern:  "*.txt",
			expected: false,
		},
		{
			name:     "path with directory",
			path:     "dir/test.txt",
			pattern:  "*.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fs.matchPattern(tt.path, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper functions and mocks

func createTestFiles(t *testing.T, tempDir string) {
	// Create directory structure:
	// tempDir/
	// ├── file0.txt
	// └── dir1/
	//     ├── file1.txt
	//     ├── file2.log
	//     └── subdir/
	//         └── file3.txt

	// Create file0.txt
	err := os.WriteFile(filepath.Join(tempDir, "file0.txt"), []byte("content0"), 0644)
	require.NoError(t, err)

	// Create dir1
	dir1 := filepath.Join(tempDir, "dir1")
	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)

	// Create file1.txt and file2.log in dir1
	err = os.WriteFile(filepath.Join(dir1, "file1.txt"), []byte("content1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir1, "file2.log"), []byte("content2"), 0644)
	require.NoError(t, err)

	// Create subdir and file3.txt
	subdir := filepath.Join(dir1, "subdir")
	err = os.MkdirAll(subdir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(subdir, "file3.txt"), []byte("content3"), 0644)
	require.NoError(t, err)
}

type mockLogger struct {
	debugLogs []string
	infoLogs  []string
	warnLogs  []string
	errorLogs []string
}

func (m *mockLogger) Debug(msg string, fields ...interface{}) {
	m.debugLogs = append(m.debugLogs, msg)
}

func (m *mockLogger) Info(msg string, fields ...interface{}) {
	m.infoLogs = append(m.infoLogs, msg)
}

func (m *mockLogger) Warn(msg string, fields ...interface{}) {
	m.warnLogs = append(m.warnLogs, msg)
}

func (m *mockLogger) Error(msg string, fields ...interface{}) {
	m.errorLogs = append(m.errorLogs, msg)
}

func (m *mockLogger) WithFields(fields map[string]interface{}) common.Logger {
	return m
}
