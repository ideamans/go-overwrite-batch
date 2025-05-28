package backlog

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ideamans/go-unified-overwright-batch-flow/l10n"
)

// FileInfo represents information about a file for backlog operations
type FileInfo interface {
	GetRelPath() string
	GetAbsPath() string
	GetSize() int64
	GetModTime() int64
}

// Logger interface for structured logging
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
}

// GzipBacklogManager implements BacklogManager with gzip compression
type GzipBacklogManager struct {
	filePath string
	logger   Logger
}

// NewGzipBacklogManager creates a new GzipBacklogManager for the specified file path
func NewGzipBacklogManager(filePath string) *GzipBacklogManager {
	return &GzipBacklogManager{
		filePath: filePath,
	}
}

// SetLogger sets the logger for the backlog manager
func (g *GzipBacklogManager) SetLogger(logger Logger) {
	g.logger = logger
}

// log writes a log message if logger is available
func (g *GzipBacklogManager) log(level string, msg string, fields ...interface{}) {
	if g.logger == nil {
		return
	}
	
	switch level {
	case "info":
		g.logger.Info(msg, fields...)
	case "error":
		g.logger.Error(msg, fields...)
	case "debug":
		g.logger.Debug(msg, fields...)
	case "warn":
		g.logger.Warn(msg, fields...)
	}
}

// StartWriting writes file entries to the configured backlog file with gzip compression
func (g *GzipBacklogManager) StartWriting(ctx context.Context, entries <-chan FileInfo) error {
	g.log("info", l10n.T("Starting to write backlog file"), "file_path", g.filePath)
	
	// Ensure directory exists
	dir := filepath.Dir(g.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		g.log("error", l10n.T("Failed to create backlog directory"), "dir", dir, "error", err)
		return fmt.Errorf("failed to create backlog directory %s: %w", dir, err)
	}
	
	// Create the backlog file
	file, err := os.Create(g.filePath)
	if err != nil {
		g.log("error", l10n.T("Failed to create backlog file"), "file_path", g.filePath, "error", err)
		return fmt.Errorf("failed to create backlog file %s: %w", g.filePath, err)
	}
	defer file.Close()
	
	// Create gzip writer
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	
	// Create buffered writer for better performance
	bufWriter := bufio.NewWriter(gzipWriter)
	defer bufWriter.Flush()
	
	encoder := json.NewEncoder(bufWriter)
	entryCount := int64(0)
	
	for {
		select {
		case <-ctx.Done():
			g.log("warn", l10n.T("Backlog writing cancelled"), "entries_written", entryCount)
			return ctx.Err()
		case entry, ok := <-entries:
			if !ok {
				// Channel closed, we're done
				g.log("info", l10n.T("Backlog writing completed"), "entries_written", entryCount, "file_path", g.filePath)
				return nil
			}
			
			// Convert to serializable struct
			backlogEntry := struct {
				RelPath string `json:"rel_path"`
				AbsPath string `json:"abs_path"`
				Size    int64  `json:"size"`
				ModTime int64  `json:"mod_time"`
			}{
				RelPath: entry.GetRelPath(),
				AbsPath: entry.GetAbsPath(),
				Size:    entry.GetSize(),
				ModTime: entry.GetModTime(),
			}
			
			if err := encoder.Encode(backlogEntry); err != nil {
				g.log("error", l10n.T("Failed to encode backlog entry"), "rel_path", entry.GetRelPath(), "error", err)
				return fmt.Errorf("failed to encode backlog entry %s: %w", entry.GetRelPath(), err)
			}
			
			entryCount++
			
			if entryCount%10000 == 0 {
				g.log("debug", l10n.T("Backlog writing progress"), "entries_written", entryCount)
			}
		}
	}
}

// StartReading reads file entries from the configured backlog file with gzip decompression
func (g *GzipBacklogManager) StartReading(ctx context.Context) (<-chan FileInfo, error) {
	g.log("info", l10n.T("Starting to read backlog file"), "file_path", g.filePath)
	
	// Check if file exists
	if _, err := os.Stat(g.filePath); os.IsNotExist(err) {
		g.log("error", l10n.T("Backlog file does not exist"), "file_path", g.filePath)
		return nil, fmt.Errorf("backlog file does not exist: %s", g.filePath)
	}
	
	// Open the backlog file
	file, err := os.Open(g.filePath)
	if err != nil {
		g.log("error", l10n.T("Failed to open backlog file"), "file_path", g.filePath, "error", err)
		return nil, fmt.Errorf("failed to open backlog file %s: %w", g.filePath, err)
	}
	
	// Create gzip reader
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		g.log("error", l10n.T("Failed to create gzip reader"), "file_path", g.filePath, "error", err)
		return nil, fmt.Errorf("failed to create gzip reader for %s: %w", g.filePath, err)
	}
	
	// Create buffered reader for better performance
	bufReader := bufio.NewReader(gzipReader)
	decoder := json.NewDecoder(bufReader)
	
	// Create channel for file entries
	entryChan := make(chan FileInfo, 100)
	
	// Start goroutine to read entries
	go func() {
		defer close(entryChan)
		defer gzipReader.Close()
		defer file.Close()
		
		entryCount := int64(0)
		
		for {
			select {
			case <-ctx.Done():
				g.log("warn", l10n.T("Backlog reading cancelled"), "entries_read", entryCount)
				return
			default:
				var backlogEntry struct {
					RelPath string `json:"rel_path"`
					AbsPath string `json:"abs_path"`
					Size    int64  `json:"size"`
					ModTime int64  `json:"mod_time"`
				}
				
				if err := decoder.Decode(&backlogEntry); err != nil {
					if err == io.EOF {
						// End of file reached
						g.log("info", l10n.T("Backlog reading completed"), "entries_read", entryCount, "file_path", g.filePath)
						return
					}
					g.log("error", l10n.T("Failed to decode backlog entry"), "error", err, "entries_read", entryCount)
					return
				}
				
				// Convert to FileInfo implementation
				fileInfo := &backlogFileInfo{
					relPath: backlogEntry.RelPath,
					absPath: backlogEntry.AbsPath,
					size:    backlogEntry.Size,
					modTime: backlogEntry.ModTime,
				}
				
				select {
				case <-ctx.Done():
					g.log("warn", l10n.T("Backlog reading cancelled"), "entries_read", entryCount)
					return
				case entryChan <- fileInfo:
					entryCount++
					
					if entryCount%10000 == 0 {
						g.log("debug", l10n.T("Backlog reading progress"), "entries_read", entryCount)
					}
				}
			}
		}
	}()
	
	return entryChan, nil
}

// CountEntries counts total entries in the configured backlog file
func (g *GzipBacklogManager) CountEntries(ctx context.Context) (int64, error) {
	g.log("info", l10n.T("Starting to count backlog entries"), "file_path", g.filePath)
	
	// Check if file exists
	if _, err := os.Stat(g.filePath); os.IsNotExist(err) {
		g.log("error", l10n.T("Backlog file does not exist"), "file_path", g.filePath)
		return 0, fmt.Errorf("backlog file does not exist: %s", g.filePath)
	}
	
	// Open the backlog file
	file, err := os.Open(g.filePath)
	if err != nil {
		g.log("error", l10n.T("Failed to open backlog file"), "file_path", g.filePath, "error", err)
		return 0, fmt.Errorf("failed to open backlog file %s: %w", g.filePath, err)
	}
	defer file.Close()
	
	// Create gzip reader
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		g.log("error", l10n.T("Failed to create gzip reader"), "file_path", g.filePath, "error", err)
		return 0, fmt.Errorf("failed to create gzip reader for %s: %w", g.filePath, err)
	}
	defer gzipReader.Close()
	
	// Create buffered reader for better performance
	bufReader := bufio.NewReader(gzipReader)
	decoder := json.NewDecoder(bufReader)
	
	var count int64
	
	for {
		select {
		case <-ctx.Done():
			g.log("warn", l10n.T("Backlog counting cancelled"), "entries_counted", count)
			return count, ctx.Err()
		default:
			var entry struct{}
			if err := decoder.Decode(&entry); err != nil {
				if err == io.EOF {
					// End of file reached
					g.log("info", l10n.T("Backlog counting completed"), "total_entries", count, "file_path", g.filePath)
					return count, nil
				}
				g.log("error", l10n.T("Failed to decode backlog entry during counting"), "error", err, "entries_counted", count)
				return count, fmt.Errorf("failed to decode backlog entry: %w", err)
			}
			count++
			
			if count%10000 == 0 {
				g.log("debug", l10n.T("Backlog counting progress"), "entries_counted", count)
			}
		}
	}
}

// backlogFileInfo implements FileInfo interface for backlog entries
type backlogFileInfo struct {
	relPath string
	absPath string
	size    int64
	modTime int64
}

func (b *backlogFileInfo) GetRelPath() string {
	return b.relPath
}

func (b *backlogFileInfo) GetAbsPath() string {
	return b.absPath
}

func (b *backlogFileInfo) GetSize() int64 {
	return b.size
}

func (b *backlogFileInfo) GetModTime() int64 {
	return b.modTime
}