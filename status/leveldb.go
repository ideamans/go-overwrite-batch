package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ideamans/go-l10n"
	uobf "github.com/ideamans/go-overwrite-batch"
	"github.com/ideamans/go-overwrite-batch/common"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// LevelDBStatusMemory implements StatusMemory interface using LevelDB for persistent storage
// It uses FileInfo.RelPath as the key and compares ModTime and Size to determine
// if a file needs processing. Data is persisted to disk using LevelDB.
type LevelDBStatusMemory struct {
	db     *leveldb.DB
	logger common.Logger
	dbPath string
}

// NewLevelDBStatusMemory creates a new LevelDBStatusMemory instance
// dbPath: absolute path to the LevelDB database directory
func NewLevelDBStatusMemory(dbPath string) (*LevelDBStatusMemory, error) {
	// Ensure the parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf(l10n.T("failed to create database directory: %w"), err)
	}

	// Open LevelDB with fsync disabled for better performance
	opts := &opt.Options{
		NoSync: true, // Disable fsync for better performance
	}

	db, err := leveldb.OpenFile(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf(l10n.T("failed to open LevelDB database: %w"), err)
	}

	return &LevelDBStatusMemory{
		db:     db,
		logger: &common.NoOpLogger{},
		dbPath: dbPath,
	}, nil
}

// Close closes the LevelDB database
func (l *LevelDBStatusMemory) Close() error {
	if l.db != nil {
		err := l.db.Close()
		l.db = nil // Prevent future operations after close
		return err
	}
	return nil
}

// SetLogger sets the logger for the LevelDB status memory
func (l *LevelDBStatusMemory) SetLogger(logger common.Logger) {
	l.logger = logger
}

// NeedsProcessing determines which files need processing
// A file needs processing if:
// 1. No status exists for the RelPath
// 2. ModTime is different from stored value
// 3. Size is different from stored value
func (l *LevelDBStatusMemory) NeedsProcessing(ctx context.Context, entries <-chan uobf.FileInfo) (<-chan uobf.FileInfo, error) {
	l.logger.Debug(l10n.T("Starting needs processing evaluation"))

	resultCh := make(chan uobf.FileInfo)

	go func() {
		defer close(resultCh)

		processedCount := 0
		needsProcessingCount := 0

		for {
			select {
			case <-ctx.Done():
				l.logger.Debug(l10n.T("NeedsProcessing cancelled by context"),
					"processed", processedCount,
					"needs_processing", needsProcessingCount)
				return
			case fileInfo, ok := <-entries:
				if !ok {
					l.logger.Info(l10n.T("NeedsProcessing completed"),
						"total_processed", processedCount,
						"needs_processing", needsProcessingCount)
					return
				}

				processedCount++

				needsProcessing, err := l.needsProcessing(fileInfo)
				if err != nil {
					l.logger.Error(l10n.T("Error checking if file needs processing"),
						"rel_path", fileInfo.RelPath,
						"error", err.Error())
					continue
				}

				if needsProcessing {
					needsProcessingCount++
					l.logger.Debug(l10n.T("File needs processing"),
						"rel_path", fileInfo.RelPath,
						"size", fileInfo.Size,
						"mod_time", fileInfo.ModTime)

					select {
					case resultCh <- fileInfo:
					case <-ctx.Done():
						l.logger.Debug(l10n.T("NeedsProcessing cancelled while sending result"))
						return
					}
				} else {
					l.logger.Debug(l10n.T("File does not need processing"),
						"rel_path", fileInfo.RelPath)
				}
			}
		}
	}()

	return resultCh, nil
}

// needsProcessing checks if a single file needs processing (internal method)
func (l *LevelDBStatusMemory) needsProcessing(fileInfo uobf.FileInfo) (bool, error) {
	data, err := l.db.Get([]byte(fileInfo.RelPath), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			// No status exists, needs processing
			return true, nil
		}
		return false, fmt.Errorf(l10n.T("failed to get status from database: %w"), err)
	}

	var status FileStatus
	if err := json.Unmarshal(data, &status); err != nil {
		l.logger.Warn(l10n.T("Failed to unmarshal status data, treating as needs processing"),
			"rel_path", fileInfo.RelPath,
			"error", err.Error())
		return true, nil
	}

	// Check if ModTime or Size has changed
	if !status.ModTime.Equal(fileInfo.ModTime) || status.Size != fileInfo.Size {
		return true, nil
	}

	// File hasn't changed and was already processed successfully
	return !status.Processed || status.LastError != "", nil
}

// ReportDone reports successful completion of file processing
func (l *LevelDBStatusMemory) ReportDone(ctx context.Context, fileInfo uobf.FileInfo) error {
	l.logger.Debug(l10n.T("Reporting file processing done"),
		"rel_path", fileInfo.RelPath,
		"size", fileInfo.Size)

	status := &FileStatus{
		RelPath:     fileInfo.RelPath,
		Size:        fileInfo.Size,
		ModTime:     fileInfo.ModTime,
		Processed:   true,
		LastError:   "",
		ProcessedAt: time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf(l10n.T("failed to marshal status data: %w"), err)
	}

	if err := l.db.Put([]byte(fileInfo.RelPath), data, nil); err != nil {
		return fmt.Errorf(l10n.T("failed to store status in database: %w"), err)
	}

	l.logger.Info(l10n.T("File processing completed successfully"),
		"rel_path", fileInfo.RelPath)

	return nil
}

// ReportError reports an error that occurred during file processing
func (l *LevelDBStatusMemory) ReportError(ctx context.Context, fileInfo uobf.FileInfo, processingErr error) error {
	l.logger.Debug(l10n.T("Reporting file processing error"),
		"rel_path", fileInfo.RelPath,
		"error", processingErr.Error())

	status := &FileStatus{
		RelPath:     fileInfo.RelPath,
		Size:        fileInfo.Size,
		ModTime:     fileInfo.ModTime,
		Processed:   false,
		LastError:   processingErr.Error(),
		ProcessedAt: time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf(l10n.T("failed to marshal error status data: %w"), err)
	}

	if err := l.db.Put([]byte(fileInfo.RelPath), data, nil); err != nil {
		return fmt.Errorf(l10n.T("failed to store error status in database: %w"), err)
	}

	l.logger.Error(l10n.T("File processing failed"),
		"rel_path", fileInfo.RelPath,
		"error", processingErr.Error())

	return nil
}

// GetStatus returns the current status of a file (useful for testing and debugging)
func (l *LevelDBStatusMemory) GetStatus(relPath string) (*FileStatus, bool) {
	data, err := l.db.Get([]byte(relPath), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, false
		}
		l.logger.Error(l10n.T("Failed to get status from database"),
			"rel_path", relPath,
			"error", err.Error())
		return nil, false
	}

	var status FileStatus
	if err := json.Unmarshal(data, &status); err != nil {
		l.logger.Error(l10n.T("Failed to unmarshal status data"),
			"rel_path", relPath,
			"error", err.Error())
		return nil, false
	}

	// Return a copy to avoid external modification
	statusCopy := status
	return &statusCopy, true
}

// GetAllStatus returns all stored file statuses (useful for testing and debugging)
func (l *LevelDBStatusMemory) GetAllStatus() map[string]*FileStatus {
	result := make(map[string]*FileStatus)

	iter := l.db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := string(iter.Key())
		var status FileStatus

		if err := json.Unmarshal(iter.Value(), &status); err != nil {
			l.logger.Warn(l10n.T("Failed to unmarshal status data during GetAllStatus"),
				"rel_path", key,
				"error", err.Error())
			continue
		}

		// Store a copy to avoid external modification
		statusCopy := status
		result[key] = &statusCopy
	}

	if err := iter.Error(); err != nil {
		l.logger.Error(l10n.T("Iterator error during GetAllStatus"),
			"error", err.Error())
	}

	return result
}

// Clear removes all stored status information (useful for testing)
func (l *LevelDBStatusMemory) Clear() error {
	// Delete all keys by iterating and deleting
	iter := l.db.NewIterator(nil, nil)
	defer iter.Release()

	batch := new(leveldb.Batch)
	for iter.Next() {
		batch.Delete(iter.Key())
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf(l10n.T("iterator error during clear: %w"), err)
	}

	if err := l.db.Write(batch, nil); err != nil {
		return fmt.Errorf(l10n.T("failed to clear database: %w"), err)
	}

	l.logger.Debug(l10n.T("All status information cleared"))
	return nil
}

// Count returns the number of files tracked in status memory
func (l *LevelDBStatusMemory) Count() int {
	count := 0

	iter := l.db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		l.logger.Error(l10n.T("Iterator error during count"),
			"error", err.Error())
		return 0
	}

	return count
}

// init registers Japanese translations for LevelDB status messages
func init() {
	l10n.Register("ja", l10n.LexiconMap{
		"failed to create database directory: %w":                       "データベースディレクトリの作成に失敗しました: %w",
		"failed to open LevelDB database: %w":                           "LevelDBデータベースのオープンに失敗しました: %w",
		"Error checking if file needs processing":                       "ファイルの処理要否チェック中にエラーが発生しました",
		"failed to get status from database: %w":                        "データベースからステータスの取得に失敗しました: %w",
		"Failed to unmarshal status data, treating as needs processing": "ステータスデータのアンマーシャルに失敗しました、処理が必要として扱います",
		"failed to marshal status data: %w":                             "ステータスデータのマーシャルに失敗しました: %w",
		"failed to store status in database: %w":                        "データベースへのステータス保存に失敗しました: %w",
		"failed to marshal error status data: %w":                       "エラーステータスデータのマーシャルに失敗しました: %w",
		"failed to store error status in database: %w":                  "データベースへのエラーステータス保存に失敗しました: %w",
		"Failed to get status from database":                            "データベースからステータスの取得に失敗しました",
		"Failed to unmarshal status data":                               "ステータスデータのアンマーシャルに失敗しました",
		"Failed to unmarshal status data during GetAllStatus":           "GetAllStatus中にステータスデータのアンマーシャルに失敗しました",
		"Iterator error during GetAllStatus":                            "GetAllStatus中にイテレーターエラーが発生しました",
		"iterator error during clear: %w":                               "クリア中にイテレーターエラーが発生しました: %w",
		"failed to clear database: %w":                                  "データベースのクリアに失敗しました: %w",
		"Iterator error during count":                                   "カウント中にイテレーターエラーが発生しました",
	})
}
