package common

import (
	"context"
	"errors"
	"testing"
	"time"
)

// testRetryableError implements RetryableError for testing
type testRetryableError struct {
	message   string
	retryable bool
}

func (e *testRetryableError) Error() string {
	return e.message
}

func (e *testRetryableError) IsRetryable() bool {
	return e.retryable
}

func TestRetryExecutor_SuccessfulOperation(t *testing.T) {
	executor := &RetryExecutor{
		MaxRetries: 3,
		Delay:      time.Millisecond * 10,
	}

	called := 0
	operation := func() error {
		called++
		return nil
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if called != 1 {
		t.Errorf("Expected operation to be called once, got: %d", called)
	}
}

func TestRetryExecutor_RetryableError(t *testing.T) {
	executor := &RetryExecutor{
		MaxRetries: 2,
		Delay:      time.Millisecond * 10,
	}

	called := 0
	operation := func() error {
		called++
		if called < 3 {
			return &testRetryableError{message: "retryable error", retryable: true}
		}
		return nil
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nil {
		t.Errorf("Expected no error after retries, got: %v", err)
	}

	if called != 3 {
		t.Errorf("Expected operation to be called 3 times, got: %d", called)
	}
}

func TestRetryExecutor_NonRetryableError(t *testing.T) {
	executor := &RetryExecutor{
		MaxRetries: 3,
		Delay:      time.Millisecond * 10,
	}

	called := 0
	expectedErr := &testRetryableError{message: "non-retryable error", retryable: false}
	operation := func() error {
		called++
		return expectedErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != expectedErr {
		t.Errorf("Expected error %v, got: %v", expectedErr, err)
	}

	if called != 1 {
		t.Errorf("Expected operation to be called once, got: %d", called)
	}
}

func TestRetryExecutor_RegularError(t *testing.T) {
	executor := &RetryExecutor{
		MaxRetries: 3,
		Delay:      time.Millisecond * 10,
	}

	called := 0
	expectedErr := errors.New("regular error")
	operation := func() error {
		called++
		return expectedErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != expectedErr {
		t.Errorf("Expected error %v, got: %v", expectedErr, err)
	}

	if called != 1 {
		t.Errorf("Expected operation to be called once, got: %d", called)
	}
}

func TestRetryExecutor_ContextCancellation(t *testing.T) {
	executor := &RetryExecutor{
		MaxRetries: 10,
		Delay:      time.Millisecond * 100,
	}

	called := 0
	operation := func() error {
		called++
		return &testRetryableError{message: "retryable error", retryable: true}
	}

	ctx, cancel := context.WithCancel(context.Background())
	
	// Cancel context after first call
	go func() {
		time.Sleep(time.Millisecond * 50)
		cancel()
	}()

	err := executor.Execute(ctx, operation)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	if called != 1 {
		t.Errorf("Expected operation to be called once before cancellation, got: %d", called)
	}
}

func TestNoOpLogger(t *testing.T) {
	logger := &NoOpLogger{}

	// These should not panic or cause any issues
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// WithFields should return the same logger
	withFields := logger.WithFields(map[string]interface{}{"key": "value"})
	if withFields != logger {
		t.Error("Expected WithFields to return the same NoOpLogger instance")
	}
}