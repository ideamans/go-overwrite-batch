package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
	uobf "github.com/ideamans/go-unified-overwrite-batch-flow"
)

// LocalFileSystem implements FileSystem interface for local filesystem operations
type LocalFileSystem struct {
	rootPath string
	logger   common.Logger
}

// NewLocalFileSystem creates a new LocalFileSystem instance with the specified root path
func NewLocalFileSystem(rootPath string) *LocalFileSystem {
	return &LocalFileSystem{
		rootPath: filepath.Clean(rootPath),
		logger:   &common.NoOpLogger{},
	}
}

// Close closes the filesystem connection (no-op for local filesystem)
func (l *LocalFileSystem) Close() error {
	return nil
}

// SetLogger sets the logger for the local filesystem
func (l *LocalFileSystem) SetLogger(logger common.Logger) {
	l.logger = logger
}

// Walk traverses directories with options and sends FileInfo to the channel
func (l *LocalFileSystem) Walk(ctx context.Context, options uobf.WalkOptions, ch chan<- uobf.FileInfo) error {
	l.logger.Debug("Starting local filesystem walk", "root_path", l.rootPath, "options", options)

	// Validate root path
	if _, err := os.Stat(l.rootPath); err != nil {
		l.logger.Error("Root path does not exist", "path", l.rootPath, "error", err)
		return fmt.Errorf("root path does not exist: %w", err)
	}

	return filepath.WalkDir(l.rootPath, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			l.logger.Debug("Walk cancelled by context", "path", path)
			return ctx.Err()
		default:
		}

		if err != nil {
			l.logger.Warn("Error walking path", "path", path, "error", err)
			return nil // Continue walking even if individual files fail
		}

		// Skip directories if FilesOnly is true
		if options.FilesOnly && d.IsDir() {
			return nil
		}

		// Check max depth (only if MaxDepth is explicitly set > 0)
		if options.MaxDepth > 0 {
			relPath, err := filepath.Rel(l.rootPath, path)
			if err != nil {
				l.logger.Warn("Failed to get relative path", "path", path, "error", err)
				return nil
			}
			depth := strings.Count(relPath, string(os.PathSeparator))
			if depth > options.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip symlinks if not following them
		if !options.FollowSymlinks {
			if info, err := d.Info(); err == nil && info.Mode()&os.ModeSymlink != 0 {
				l.logger.Debug("Skipping symlink", "path", path)
				return nil
			}
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			l.logger.Warn("Failed to get file info", "path", path, "error", err)
			return nil
		}

		// Create relative path for FileInfo
		relPath, err := filepath.Rel(l.rootPath, path)
		if err != nil {
			l.logger.Warn("Failed to get relative path", "path", path, "error", err)
			return nil
		}

		// Skip root directory itself (empty relative path ".")
		if relPath == "." {
			return nil
		}

		fileInfo := uobf.FileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    uint32(info.Mode()),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
			RelPath: filepath.ToSlash(relPath), // Convert to forward slashes for consistency
			AbsPath: path,
		}

		// Apply include/exclude filters
		if l.shouldIncludeFile(fileInfo, options) {
			l.logger.Debug("Sending file to channel", "rel_path", fileInfo.RelPath, "abs_path", fileInfo.AbsPath, "size", fileInfo.Size)
			select {
			case ch <- fileInfo:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			l.logger.Debug("File filtered out", "rel_path", fileInfo.RelPath)
		}

		return nil
	})
}

// Download downloads a file from the filesystem to local path (essentially a copy operation)
func (l *LocalFileSystem) Download(ctx context.Context, remotePath, localPath string) error {
	l.logger.Debug("Starting file download", "remote_path", remotePath, "local_path", localPath)

	// Clean paths
	remotePath = filepath.Clean(remotePath)
	localPath = filepath.Clean(localPath)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		l.logger.Error("Failed to create destination directory", "dir", filepath.Dir(localPath), "error", err)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open source file
	src, err := os.Open(remotePath)
	if err != nil {
		l.logger.Error("Failed to open source file", "path", remotePath, "error", err)
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(localPath)
	if err != nil {
		l.logger.Error("Failed to create destination file", "path", localPath, "error", err)
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// Copy file content with context checking
	_, err = l.copyWithContext(ctx, dst, src)
	if err != nil {
		l.logger.Error("Failed to copy file content", "remote_path", remotePath, "local_path", localPath, "error", err)
		// Clean up partial file on error
		os.Remove(localPath)
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	l.logger.Info("File downloaded successfully", "remote_path", remotePath, "local_path", localPath)
	return nil
}

// Upload uploads a local file to the filesystem and returns the uploaded file info (essentially a copy operation)
func (l *LocalFileSystem) Upload(ctx context.Context, localPath, remotePath string) (*uobf.FileInfo, error) {
	l.logger.Debug("Starting file upload", "local_path", localPath, "remote_path", remotePath)

	// Clean paths
	localPath = filepath.Clean(localPath)
	remotePath = filepath.Clean(remotePath)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(remotePath), 0755); err != nil {
		l.logger.Error("Failed to create destination directory", "dir", filepath.Dir(remotePath), "error", err)
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Check if source file exists
	if _, err := os.Stat(localPath); err != nil {
		l.logger.Error("Failed to stat source file", "path", localPath, "error", err)
		return nil, fmt.Errorf("failed to stat source file: %w", err)
	}

	// Open source file
	src, err := os.Open(localPath)
	if err != nil {
		l.logger.Error("Failed to open source file", "path", localPath, "error", err)
		return nil, fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(remotePath)
	if err != nil {
		l.logger.Error("Failed to create destination file", "path", remotePath, "error", err)
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// Copy file content with context checking
	size, err := l.copyWithContext(ctx, dst, src)
	if err != nil {
		l.logger.Error("Failed to copy file content", "local_path", localPath, "remote_path", remotePath, "error", err)
		// Clean up partial file on error
		os.Remove(remotePath)
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	// Get uploaded file info
	uploadedInfo, err := os.Stat(remotePath)
	if err != nil {
		l.logger.Error("Failed to stat uploaded file", "path", remotePath, "error", err)
		return nil, fmt.Errorf("failed to stat uploaded file: %w", err)
	}

	// Create relative path for uploaded file
	relPath, err := filepath.Rel(l.rootPath, remotePath)
	if err != nil {
		l.logger.Warn("Failed to get relative path for uploaded file", "path", remotePath, "error", err)
		relPath = remotePath // fallback to absolute path
	}

	fileInfo := &uobf.FileInfo{
		Name:    uploadedInfo.Name(),
		Size:    uploadedInfo.Size(),
		Mode:    uint32(uploadedInfo.Mode()),
		ModTime: uploadedInfo.ModTime(),
		IsDir:   uploadedInfo.IsDir(),
		RelPath: filepath.ToSlash(relPath),
		AbsPath: remotePath,
	}

	l.logger.Info("File uploaded successfully", "local_path", localPath, "remote_path", remotePath, "size", size)
	return fileInfo, nil
}

// copyWithContext copies data from src to dst while checking for context cancellation
func (l *LocalFileSystem) copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64

	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = fmt.Errorf("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er != io.EOF {
				return written, er
			}
			break
		}
	}
	return written, nil
}

// shouldIncludeFile checks if a file should be included based on include/exclude patterns
func (l *LocalFileSystem) shouldIncludeFile(fileInfo uobf.FileInfo, options uobf.WalkOptions) bool {
	// TODO: Implement pattern matching using open-match.dev/open-match
	// For now, simple implementation that includes all files

	// If include patterns are specified, file must match at least one
	if len(options.Include) > 0 {
		matched := false
		for _, pattern := range options.Include {
			if l.matchPattern(fileInfo.RelPath, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// If exclude patterns are specified, file must not match any
	if len(options.Exclude) > 0 {
		for _, pattern := range options.Exclude {
			if l.matchPattern(fileInfo.RelPath, pattern) {
				return false
			}
		}
	}

	return true
}

// matchPattern performs simple pattern matching (to be replaced with open-match implementation)
func (l *LocalFileSystem) matchPattern(path, pattern string) bool {
	// Simple glob-like matching for now
	// TODO: Replace with open-match.dev/open-match implementation
	matched, err := filepath.Match(pattern, filepath.Base(path))
	if err != nil {
		l.logger.Warn("Pattern matching error", "pattern", pattern, "path", path, "error", err)
		return false
	}
	return matched
}
