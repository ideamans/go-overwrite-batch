package backlog

import (
	"context"
	"fmt"
	"sync"

	"github.com/ideamans/go-unified-overwrite-batch-flow/common"
	"github.com/ideamans/go-unified-overwrite-batch-flow/l10n"
)




func init() {
	// Register Japanese translations for memory backlog operations
	l10n.Register("ja", l10n.LexiconMap{
		"Starting to write memory backlog":           "メモリバックログの書き込みを開始します",
		"Memory backlog writing cancelled":           "メモリバックログの書き込みがキャンセルされました",
		"Memory backlog writing completed":           "メモリバックログの書き込みが完了しました",
		"Memory backlog writing progress":            "メモリバックログ書き込み進行状況",
		"Starting to read memory backlog":            "メモリバックログの読み取りを開始します",
		"Memory backlog is empty":                    "メモリバックログは空です",
		"Memory backlog reading cancelled":           "メモリバックログの読み取りがキャンセルされました",
		"Memory backlog reading completed":           "メモリバックログの読み取りが完了しました",
		"Memory backlog reading progress":            "メモリバックログ読み取り進行状況",
		"Starting to count memory backlog entries":   "メモリバックログエントリの集計を開始します",
		"Memory backlog counting completed":          "メモリバックログの集計が完了しました",
	})
}

// MemoryBacklogManager implements BacklogManager using in-memory storage
// This implementation is primarily intended for testing and development
type MemoryBacklogManager struct {
	mu       sync.RWMutex
	entries  []string
	logger   common.Logger
}

// NewMemoryBacklogManager creates a new in-memory backlog manager
func NewMemoryBacklogManager() *MemoryBacklogManager {
	return &MemoryBacklogManager{
		entries: make([]string, 0),
	}
}

// SetLogger sets the logger for the backlog manager
func (m *MemoryBacklogManager) SetLogger(logger common.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger
}

// log writes a log message if logger is available
func (m *MemoryBacklogManager) log(level string, msg string, fields ...interface{}) {
	m.mu.RLock()
	logger := m.logger
	m.mu.RUnlock()
	
	if logger == nil {
		return
	}
	
	switch level {
	case "info":
		logger.Info(msg, fields...)
	case "error":
		logger.Error(msg, fields...)
	case "debug":
		logger.Debug(msg, fields...)
	case "warn":
		logger.Warn(msg, fields...)
	}
}

// StartWriting writes relative file paths to the in-memory storage
func (m *MemoryBacklogManager) StartWriting(ctx context.Context, relPaths <-chan string) error {
	m.log("info", l10n.T("Starting to write memory backlog"))
	
	// Clear existing entries and prepare for new ones
	m.mu.Lock()
	m.entries = m.entries[:0] // Clear slice while keeping capacity
	m.mu.Unlock()
	
	entryCount := int64(0)
	
	for {
		select {
		case <-ctx.Done():
			m.log("warn", l10n.T("Memory backlog writing cancelled"), "entries_written", entryCount)
			return ctx.Err()
		case relPath, ok := <-relPaths:
			if !ok {
				// Channel closed, we're done
				m.log("info", l10n.T("Memory backlog writing completed"), "entries_written", entryCount)
				return nil
			}
			
			// Add relative path to memory storage
			m.mu.Lock()
			m.entries = append(m.entries, relPath)
			m.mu.Unlock()
			
			entryCount++
			
			if entryCount%10000 == 0 {
				m.log("debug", l10n.T("Memory backlog writing progress"), "entries_written", entryCount)
			}
		}
	}
}

// StartReading reads relative file paths from the in-memory storage
func (m *MemoryBacklogManager) StartReading(ctx context.Context) (<-chan string, error) {
	m.log("info", l10n.T("Starting to read memory backlog"))
	
	// Get snapshot of entries
	m.mu.RLock()
	entriesSnapshot := make([]string, len(m.entries))
	copy(entriesSnapshot, m.entries)
	m.mu.RUnlock()
	
	if len(entriesSnapshot) == 0 {
		m.log("info", l10n.T("Memory backlog is empty"))
	}
	
	// Create channel for relative paths
	relPathChan := make(chan string, 100)
	
	// Start goroutine to read entries
	go func() {
		defer close(relPathChan)
		
		entryCount := int64(0)
		
		for _, relPath := range entriesSnapshot {
			select {
			case <-ctx.Done():
				m.log("warn", l10n.T("Memory backlog reading cancelled"), "entries_read", entryCount)
				return
			case relPathChan <- relPath:
				entryCount++
				
				if entryCount%10000 == 0 {
					m.log("debug", l10n.T("Memory backlog reading progress"), "entries_read", entryCount)
				}
			}
		}
		
		m.log("info", l10n.T("Memory backlog reading completed"), "entries_read", entryCount)
	}()
	
	return relPathChan, nil
}

// CountRelPaths counts total relative paths in the in-memory storage
func (m *MemoryBacklogManager) CountRelPaths(ctx context.Context) (int64, error) {
	m.log("info", l10n.T("Starting to count memory backlog entries"))
	
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	
	m.mu.RLock()
	count := int64(len(m.entries))
	m.mu.RUnlock()
	
	m.log("info", l10n.T("Memory backlog counting completed"), "total_entries", count)
	return count, nil
}

// Clear removes all entries from the memory backlog
// This method is useful for testing and cleanup
func (m *MemoryBacklogManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = m.entries[:0]
}

// Size returns the current number of entries in the backlog
// This method is useful for testing and monitoring
func (m *MemoryBacklogManager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// GetEntries returns a copy of all entries in the backlog
// This method is useful for testing and debugging
func (m *MemoryBacklogManager) GetEntries() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	entries := make([]string, len(m.entries))
	copy(entries, m.entries)
	return entries
}

// SetEntries replaces all entries in the backlog with the provided ones
// This method is useful for testing setup
func (m *MemoryBacklogManager) SetEntries(entries []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.entries = make([]string, len(entries))
	copy(m.entries, entries)
}

// String returns a string representation of the memory backlog for debugging
func (m *MemoryBacklogManager) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("MemoryBacklogManager{entries: %d}", len(m.entries))
}