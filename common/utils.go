package common

import (
	"context"
	"time"
)

// Logger provides a standard logging interface
// Compatible with popular loggers like logrus, zap, slog, etc.
type Logger interface {
	// Debug logs debug-level messages
	Debug(msg string, fields ...interface{})

	// Info logs info-level messages
	Info(msg string, fields ...interface{})

	// Warn logs warning-level messages
	Warn(msg string, fields ...interface{})

	// Error logs error-level messages
	Error(msg string, fields ...interface{})

	// WithFields returns a logger with additional fields
	WithFields(fields map[string]interface{}) Logger
}

// NoOpLogger provides a no-operation logger for testing or when logging is disabled
type NoOpLogger struct{}

func (l *NoOpLogger) Debug(msg string, fields ...interface{})         {}
func (l *NoOpLogger) Info(msg string, fields ...interface{})          {}
func (l *NoOpLogger) Warn(msg string, fields ...interface{})          {}
func (l *NoOpLogger) Error(msg string, fields ...interface{})         {}
func (l *NoOpLogger) WithFields(fields map[string]interface{}) Logger { return l }

// RetryableError indicates if an error should be retried
type RetryableError interface {
	error
	IsRetryable() bool
}

// NetworkError represents network-related errors that may be retried
type NetworkError struct {
	Operation   string
	Cause       error
	ShouldRetry bool
}

func (e *NetworkError) Error() string {
	return e.Operation + ": " + e.Cause.Error()
}

func (e *NetworkError) IsRetryable() bool {
	return e.ShouldRetry
}

// RetryExecutor handles retry logic for network operations
type RetryExecutor struct {
	MaxRetries int
	Delay      time.Duration
}

func (r *RetryExecutor) Execute(ctx context.Context, operation func() error) error {
	var lastErr error
	for i := 0; i <= r.MaxRetries; i++ {
		if err := operation(); err != nil {
			if retryable, ok := err.(RetryableError); ok && retryable.IsRetryable() && i < r.MaxRetries {
				lastErr = err
				select {
				case <-time.After(r.Delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}
		return nil
	}
	return lastErr
}
