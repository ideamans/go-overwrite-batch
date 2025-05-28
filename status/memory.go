package status

import (
	"context"
	"sync"
	"time"

	uobf "github.com/ideamans/go-unified-overwrite-batch-flow"
	"github.com/ideamans/go-unified-overwrite-batch-flow/l10n"
)

// FileStatus represents the processing status of a file
type FileStatus struct {
	RelPath     string    `json:"rel_path"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	Processed   bool      `json:"processed"`
	LastError   string    `json:"last_error,omitempty"`
	ProcessedAt time.Time `json:"processed_at,omitempty"`
}

// MemoryStatusMemory implements StatusMemory interface using in-memory storage
// It uses FileInfo.RelPath as the key and compares ModTime and Size to determine
// if a file needs processing
type MemoryStatusMemory struct {
	mu     sync.RWMutex
	status map[string]*FileStatus // key: RelPath, value: FileStatus
	logger uobf.Logger
}

// NewMemoryStatusMemory creates a new MemoryStatusMemory instance
func NewMemoryStatusMemory() *MemoryStatusMemory {
	return &MemoryStatusMemory{
		status: make(map[string]*FileStatus),
		logger: &uobf.NoOpLogger{},
	}
}

// SetLogger sets the logger for the memory status memory
func (m *MemoryStatusMemory) SetLogger(logger uobf.Logger) {
	m.logger = logger
}

// NeedsProcessing determines which files need processing
// A file needs processing if:
// 1. No status exists for the RelPath
// 2. ModTime is different from stored value
// 3. Size is different from stored value
func (m *MemoryStatusMemory) NeedsProcessing(ctx context.Context, entries <-chan uobf.FileInfo) (<-chan uobf.FileInfo, error) {
	m.logger.Debug(l10n.T("Starting needs processing evaluation"))

	resultCh := make(chan uobf.FileInfo)

	go func() {
		defer close(resultCh)

		processedCount := 0
		needsProcessingCount := 0

		for {
			select {
			case <-ctx.Done():
				m.logger.Debug(l10n.T("NeedsProcessing cancelled by context"),
					"processed", processedCount,
					"needs_processing", needsProcessingCount)
				return
			case fileInfo, ok := <-entries:
				if !ok {
					m.logger.Info(l10n.T("NeedsProcessing completed"),
						"total_processed", processedCount,
						"needs_processing", needsProcessingCount)
					return
				}

				processedCount++

				if m.needsProcessing(fileInfo) {
					needsProcessingCount++
					m.logger.Debug(l10n.T("File needs processing"),
						"rel_path", fileInfo.RelPath,
						"size", fileInfo.Size,
						"mod_time", fileInfo.ModTime)

					select {
					case resultCh <- fileInfo:
					case <-ctx.Done():
						m.logger.Debug(l10n.T("NeedsProcessing cancelled while sending result"))
						return
					}
				} else {
					m.logger.Debug(l10n.T("File does not need processing"),
						"rel_path", fileInfo.RelPath)
				}
			}
		}
	}()

	return resultCh, nil
}

// needsProcessing checks if a single file needs processing (internal method)
func (m *MemoryStatusMemory) needsProcessing(fileInfo uobf.FileInfo) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.status[fileInfo.RelPath]
	if !exists {
		// No status exists, needs processing
		return true
	}

	// Check if ModTime or Size has changed
	if !status.ModTime.Equal(fileInfo.ModTime) || status.Size != fileInfo.Size {
		return true
	}

	// File hasn't changed and was already processed successfully
	return !status.Processed || status.LastError != ""
}

// ReportDone reports successful completion of file processing
func (m *MemoryStatusMemory) ReportDone(ctx context.Context, fileInfo uobf.FileInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Debug(l10n.T("Reporting file processing done"),
		"rel_path", fileInfo.RelPath,
		"size", fileInfo.Size)

	m.status[fileInfo.RelPath] = &FileStatus{
		RelPath:     fileInfo.RelPath,
		Size:        fileInfo.Size,
		ModTime:     fileInfo.ModTime,
		Processed:   true,
		LastError:   "",
		ProcessedAt: time.Now(),
	}

	m.logger.Info(l10n.T("File processing completed successfully"),
		"rel_path", fileInfo.RelPath)

	return nil
}

// ReportError reports an error that occurred during file processing
func (m *MemoryStatusMemory) ReportError(ctx context.Context, fileInfo uobf.FileInfo, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Debug(l10n.T("Reporting file processing error"),
		"rel_path", fileInfo.RelPath,
		"error", err.Error())

	m.status[fileInfo.RelPath] = &FileStatus{
		RelPath:     fileInfo.RelPath,
		Size:        fileInfo.Size,
		ModTime:     fileInfo.ModTime,
		Processed:   false,
		LastError:   err.Error(),
		ProcessedAt: time.Now(),
	}

	m.logger.Error(l10n.T("File processing failed"),
		"rel_path", fileInfo.RelPath,
		"error", err.Error())

	return nil
}

// GetStatus returns the current status of a file (useful for testing and debugging)
func (m *MemoryStatusMemory) GetStatus(relPath string) (*FileStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.status[relPath]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid external modification
	statusCopy := *status
	return &statusCopy, true
}

// GetAllStatus returns all stored file statuses (useful for testing and debugging)
func (m *MemoryStatusMemory) GetAllStatus() map[string]*FileStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*FileStatus)
	for k, v := range m.status {
		statusCopy := *v
		result[k] = &statusCopy
	}

	return result
}

// Clear removes all stored status information (useful for testing)
func (m *MemoryStatusMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status = make(map[string]*FileStatus)
	m.logger.Debug(l10n.T("All status information cleared"))
}

// Count returns the number of files tracked in status memory
func (m *MemoryStatusMemory) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.status)
}

// init registers Japanese translations for memory status messages
func init() {
	l10n.Register("ja", l10n.LexiconMap{
		"Starting needs processing evaluation":           "処理要否の評価を開始します",
		"NeedsProcessing cancelled by context":           "コンテキストによりNeedsProcessingがキャンセルされました",
		"NeedsProcessing completed":                      "NeedsProcessing完了",
		"File needs processing":                          "ファイルの処理が必要です",
		"File does not need processing":                  "ファイルの処理は不要です",
		"NeedsProcessing cancelled while sending result": "結果送信中にNeedsProcessingがキャンセルされました",
		"Reporting file processing done":                 "ファイル処理完了を報告します",
		"File processing completed successfully":         "ファイル処理が正常に完了しました",
		"Reporting file processing error":                "ファイル処理エラーを報告します",
		"File processing failed":                         "ファイル処理に失敗しました",
		"All status information cleared":                 "全ステータス情報をクリアしました",
	})
}
