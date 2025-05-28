package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideamans/go-unified-overwright-batch-flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocalFileSystem(t *testing.T) {
	fs := NewLocalFileSystem()
	assert.NotNil(t, fs)
	assert.NotNil(t, fs.logger)
}

func TestLocalFileSystem_Close(t *testing.T) {
	fs := NewLocalFileSystem()
	err := fs.Close()
	assert.NoError(t, err)
}

func TestLocalFileSystem_SetLogger(t *testing.T) {
	fs := NewLocalFileSystem()
	mockLogger := &mockLogger{}
	fs.SetLogger(mockLogger)
	assert.Equal(t, mockLogger, fs.logger)
}

func TestLocalFileSystem_Walk(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

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
			fs := NewLocalFileSystem()
			fs.SetLogger(&mockLogger{})

			ch := make(chan uobf.FileInfo, 100)
			ctx := context.Background()

			// Run Walk in goroutine
			done := make(chan error, 1)
			go func() {
				defer close(ch)
				done <- fs.Walk(ctx, tt.rootPath, tt.options, ch)
			}()

			// Collect results
			var results []string
			for fileInfo := range ch {
				results = append(results, fileInfo.Path)
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
	defer os.RemoveAll(tempDir)

	createTestFiles(t, tempDir)

	fs := NewLocalFileSystem()
	ch := make(chan uobf.FileInfo, 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = fs.Walk(ctx, tempDir, uobf.WalkOptions{}, ch)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestLocalFileSystem_Download(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	content := "test content"
	err = os.WriteFile(srcFile, []byte(content), 0644)
	require.NoError(t, err)

	tests := []struct {
		name       string
		remotePath string
		localPath  string
		wantErr    bool
	}{
		{
			name:       "successful download",
			remotePath: srcFile,
			localPath:  filepath.Join(tempDir, "downloaded.txt"),
			wantErr:    false,
		},
		{
			name:       "download to subdirectory",
			remotePath: srcFile,
			localPath:  filepath.Join(tempDir, "subdir", "downloaded.txt"),
			wantErr:    false,
		},
		{
			name:       "non-existent source file",
			remotePath: filepath.Join(tempDir, "nonexistent.txt"),
			localPath:  filepath.Join(tempDir, "downloaded.txt"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewLocalFileSystem()
			fs.SetLogger(&mockLogger{})

			err := fs.Download(context.Background(), tt.remotePath, tt.localPath)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				
				// Verify file was copied correctly
				downloadedContent, err := os.ReadFile(tt.localPath)
				assert.NoError(t, err)
				assert.Equal(t, content, string(downloadedContent))
			}
		})
	}
}

func TestLocalFileSystem_Download_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	err = os.WriteFile(srcFile, []byte("test content"), 0644)
	require.NoError(t, err)

	fs := NewLocalFileSystem()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	localPath := filepath.Join(tempDir, "downloaded.txt")
	err = fs.Download(ctx, srcFile, localPath)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// Verify partial file was cleaned up
	_, err = os.Stat(localPath)
	assert.True(t, os.IsNotExist(err))
}

func TestLocalFileSystem_Upload(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	content := "test upload content"
	err = os.WriteFile(srcFile, []byte(content), 0644)
	require.NoError(t, err)

	tests := []struct {
		name       string
		localPath  string
		remotePath string
		wantErr    bool
	}{
		{
			name:       "successful upload",
			localPath:  srcFile,
			remotePath: filepath.Join(tempDir, "uploaded.txt"),
			wantErr:    false,
		},
		{
			name:       "upload to subdirectory",
			localPath:  srcFile,
			remotePath: filepath.Join(tempDir, "subdir", "uploaded.txt"),
			wantErr:    false,
		},
		{
			name:       "non-existent source file",
			localPath:  filepath.Join(tempDir, "nonexistent.txt"),
			remotePath: filepath.Join(tempDir, "uploaded.txt"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewLocalFileSystem()
			fs.SetLogger(&mockLogger{})

			fileInfo, err := fs.Upload(context.Background(), tt.localPath, tt.remotePath)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, fileInfo)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fileInfo)
				
				// Verify file info
				assert.Equal(t, filepath.Base(tt.remotePath), fileInfo.Name)
				assert.Equal(t, int64(len(content)), fileInfo.Size)
				assert.Equal(t, tt.remotePath, fileInfo.Path)
				assert.False(t, fileInfo.IsDir)

				// Verify file was copied correctly
				uploadedContent, err := os.ReadFile(tt.remotePath)
				assert.NoError(t, err)
				assert.Equal(t, content, string(uploadedContent))
			}
		})
	}
}

func TestLocalFileSystem_Upload_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "local_fs_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	err = os.WriteFile(srcFile, []byte("test content"), 0644)
	require.NoError(t, err)

	fs := NewLocalFileSystem()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	remotePath := filepath.Join(tempDir, "uploaded.txt")
	fileInfo, err := fs.Upload(ctx, srcFile, remotePath)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.Nil(t, fileInfo)

	// Verify partial file was cleaned up
	_, err = os.Stat(remotePath)
	assert.True(t, os.IsNotExist(err))
}

func TestLocalFileSystem_shouldIncludeFile(t *testing.T) {
	fs := NewLocalFileSystem()

	tests := []struct {
		name     string
		fileInfo uobf.FileInfo
		options  uobf.WalkOptions
		expected bool
	}{
		{
			name:     "no filters - include all",
			fileInfo: uobf.FileInfo{Path: "test.txt"},
			options:  uobf.WalkOptions{},
			expected: true,
		},
		{
			name:     "include pattern matches",
			fileInfo: uobf.FileInfo{Path: "test.txt"},
			options:  uobf.WalkOptions{Include: []string{"*.txt"}},
			expected: true,
		},
		{
			name:     "include pattern doesn't match",
			fileInfo: uobf.FileInfo{Path: "test.log"},
			options:  uobf.WalkOptions{Include: []string{"*.txt"}},
			expected: false,
		},
		{
			name:     "exclude pattern matches",
			fileInfo: uobf.FileInfo{Path: "test.log"},
			options:  uobf.WalkOptions{Exclude: []string{"*.log"}},
			expected: false,
		},
		{
			name:     "exclude pattern doesn't match",
			fileInfo: uobf.FileInfo{Path: "test.txt"},
			options:  uobf.WalkOptions{Exclude: []string{"*.log"}},
			expected: true,
		},
		{
			name:     "include and exclude - include wins",
			fileInfo: uobf.FileInfo{Path: "test.txt"},
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
	fs := NewLocalFileSystem()

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

func (m *mockLogger) WithFields(fields map[string]interface{}) uobf.Logger {
	return m
}