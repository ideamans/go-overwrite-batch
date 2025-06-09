package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	uobf "github.com/ideamans/overwritebatch"
	"github.com/ideamans/overwritebatch/common"
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
func (l *LocalFileSystem) Download(ctx context.Context, remoteRelPath, localFullPath string) error {
	l.logger.Debug("Starting file download", "remote_rel_path", remoteRelPath, "local_full_path", localFullPath)

	// Convert relative path to absolute path
	remoteAbsPath := filepath.Join(l.rootPath, remoteRelPath)

	// Clean paths
	remoteAbsPath = filepath.Clean(remoteAbsPath)
	localFullPath = filepath.Clean(localFullPath)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(localFullPath), 0755); err != nil {
		l.logger.Error("Failed to create destination directory", "dir", filepath.Dir(localFullPath), "error", err)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open source file
	src, err := os.Open(remoteAbsPath)
	if err != nil {
		l.logger.Error("Failed to open source file", "path", remoteAbsPath, "error", err)
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(localFullPath)
	if err != nil {
		l.logger.Error("Failed to create destination file", "path", localFullPath, "error", err)
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// Copy file content with context checking
	_, err = l.copyWithContext(ctx, dst, src)
	if err != nil {
		l.logger.Error("Failed to copy file content", "remote_abs_path", remoteAbsPath, "local_full_path", localFullPath, "error", err)
		// Clean up partial file on error
		os.Remove(localFullPath)
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	l.logger.Info("File downloaded successfully", "remote_rel_path", remoteRelPath, "local_full_path", localFullPath)
	return nil
}

// Upload uploads a local file to the filesystem and returns the uploaded file info (essentially a copy operation)
func (l *LocalFileSystem) Upload(ctx context.Context, localFullPath, remoteRelPath string) (*uobf.FileInfo, error) {
	l.logger.Debug("Starting file upload", "local_full_path", localFullPath, "remote_rel_path", remoteRelPath)

	// Convert relative path to absolute path
	remoteAbsPath := filepath.Join(l.rootPath, remoteRelPath)

	// Clean paths
	localFullPath = filepath.Clean(localFullPath)
	remoteAbsPath = filepath.Clean(remoteAbsPath)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(remoteAbsPath), 0755); err != nil {
		l.logger.Error("Failed to create destination directory", "dir", filepath.Dir(remoteAbsPath), "error", err)
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Check if source file exists
	if _, err := os.Stat(localFullPath); err != nil {
		l.logger.Error("Failed to stat source file", "path", localFullPath, "error", err)
		return nil, fmt.Errorf("failed to stat source file: %w", err)
	}

	// Open source file
	src, err := os.Open(localFullPath)
	if err != nil {
		l.logger.Error("Failed to open source file", "path", localFullPath, "error", err)
		return nil, fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(remoteAbsPath)
	if err != nil {
		l.logger.Error("Failed to create destination file", "path", remoteAbsPath, "error", err)
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// Copy file content with context checking
	size, err := l.copyWithContext(ctx, dst, src)
	if err != nil {
		l.logger.Error("Failed to copy file content", "local_full_path", localFullPath, "remote_abs_path", remoteAbsPath, "error", err)
		// Clean up partial file on error
		os.Remove(remoteAbsPath)
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	// Get uploaded file info
	uploadedInfo, err := os.Stat(remoteAbsPath)
	if err != nil {
		l.logger.Error("Failed to stat uploaded file", "path", remoteAbsPath, "error", err)
		return nil, fmt.Errorf("failed to stat uploaded file: %w", err)
	}

	fileInfo := &uobf.FileInfo{
		Name:    uploadedInfo.Name(),
		Size:    uploadedInfo.Size(),
		Mode:    uint32(uploadedInfo.Mode()),
		ModTime: uploadedInfo.ModTime(),
		IsDir:   uploadedInfo.IsDir(),
		RelPath: filepath.ToSlash(remoteRelPath),
		AbsPath: remoteAbsPath,
	}

	l.logger.Info("File uploaded successfully", "local_full_path", localFullPath, "remote_rel_path", remoteRelPath, "size", size)
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
