package backlog

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideamans/go-unified-overwrite-batch-flow/l10n"
)

// Logger interface for structured logging
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	WithFields(fields map[string]interface{}) Logger
}

func init() {
	// Register Japanese translations for backlog operations
	l10n.Register("ja", l10n.LexiconMap{
		"Starting to write backlog file":              "バックログファイルの書き込みを開始します",
		"Failed to create backlog directory":          "バックログディレクトリの作成に失敗しました",
		"Failed to create backlog file":               "バックログファイルの作成に失敗しました",
		"Backlog writing cancelled":                   "バックログの書き込みがキャンセルされました",
		"Backlog writing completed":                   "バックログの書き込みが完了しました",
		"Failed to write backlog entry":               "バックログエントリの書き込みに失敗しました",
		"Backlog writing progress":                    "バックログ書き込み進行状況",
		"Starting to read backlog file":               "バックログファイルの読み取りを開始します",
		"Backlog file does not exist":                 "バックログファイルが存在しません",
		"Failed to open backlog file":                 "バックログファイルのオープンに失敗しました",
		"Failed to create gzip reader":                "Gzipリーダーの作成に失敗しました",
		"Backlog reading cancelled":                   "バックログの読み取りがキャンセルされました",
		"Backlog reading completed":                   "バックログの読み取りが完了しました",
		"Failed to read backlog file":                 "バックログファイルの読み取りに失敗しました",
		"Backlog reading progress":                    "バックログ読み取り進行状況",
		"Starting to count backlog entries":           "バックログエントリの集計を開始します",
		"Backlog counting cancelled":                  "バックログの集計がキャンセルされました",
		"Failed to scan backlog file during counting": "集計中のバックログファイルスキャンに失敗しました",
		"Backlog counting completed":                  "バックログの集計が完了しました",
		"Backlog counting progress":                   "バックログ集計進行状況",
	})
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

// StartWriting writes relative file paths to the configured backlog file with gzip compression
func (g *GzipBacklogManager) StartWriting(ctx context.Context, relPaths <-chan string) error {
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

	entryCount := int64(0)

	for {
		select {
		case <-ctx.Done():
			g.log("warn", l10n.T("Backlog writing cancelled"), "entries_written", entryCount)
			return ctx.Err()
		case relPath, ok := <-relPaths:
			if !ok {
				// Channel closed, we're done
				g.log("info", l10n.T("Backlog writing completed"), "entries_written", entryCount, "file_path", g.filePath)
				return nil
			}

			// Write relative path as simple text line
			if _, err := bufWriter.WriteString(relPath + "\n"); err != nil {
				g.log("error", l10n.T("Failed to write backlog entry"), "rel_path", relPath, "error", err)
				return fmt.Errorf("failed to write backlog entry %s: %w", relPath, err)
			}

			entryCount++

			if entryCount%10000 == 0 {
				g.log("debug", l10n.T("Backlog writing progress"), "entries_written", entryCount)
			}
		}
	}
}

// StartReading reads relative file paths from the configured backlog file with gzip decompression
func (g *GzipBacklogManager) StartReading(ctx context.Context) (<-chan string, error) {
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
	scanner := bufio.NewScanner(bufReader)

	// Create channel for relative paths
	relPathChan := make(chan string, 100)

	// Start goroutine to read entries
	go func() {
		defer close(relPathChan)
		defer gzipReader.Close()
		defer file.Close()

		entryCount := int64(0)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				g.log("warn", l10n.T("Backlog reading cancelled"), "entries_read", entryCount)
				return
			default:
				relPath := strings.TrimSpace(scanner.Text())
				if relPath == "" {
					continue // Skip empty lines
				}

				select {
				case <-ctx.Done():
					g.log("warn", l10n.T("Backlog reading cancelled"), "entries_read", entryCount)
					return
				case relPathChan <- relPath:
					entryCount++

					if entryCount%10000 == 0 {
						g.log("debug", l10n.T("Backlog reading progress"), "entries_read", entryCount)
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			g.log("error", l10n.T("Failed to read backlog file"), "error", err, "entries_read", entryCount)
		} else {
			g.log("info", l10n.T("Backlog reading completed"), "entries_read", entryCount, "file_path", g.filePath)
		}
	}()

	return relPathChan, nil
}

// CountRelPaths counts total relative paths in the configured backlog file
func (g *GzipBacklogManager) CountRelPaths(ctx context.Context) (int64, error) {
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
	scanner := bufio.NewScanner(bufReader)

	var count int64

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			g.log("warn", l10n.T("Backlog counting cancelled"), "entries_counted", count)
			return count, ctx.Err()
		default:
			relPath := strings.TrimSpace(scanner.Text())
			if relPath != "" {
				count++

				if count%10000 == 0 {
					g.log("debug", l10n.T("Backlog counting progress"), "entries_counted", count)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		g.log("error", l10n.T("Failed to scan backlog file during counting"), "error", err, "entries_counted", count)
		return count, fmt.Errorf("failed to scan backlog file: %w", err)
	}

	g.log("info", l10n.T("Backlog counting completed"), "total_entries", count, "file_path", g.filePath)
	return count, nil
}
