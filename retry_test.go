package uobf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideamans/overwritebatch/common"
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

// testNonRetryableError is a regular error that doesn't implement RetryableError
type testNonRetryableError struct {
	message string
}

func (e *testNonRetryableError) Error() string {
	return e.message
}

func TestRetryExecutor_SuccessfulOperation(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	operation := func() error {
		callCount++
		return nil // Success
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time, got: %d", callCount)
	}
}

func TestRetryExecutor_RetryableErrorWithSuccess(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	operation := func() error {
		callCount++
		if callCount < 3 {
			return &testRetryableError{
				message:   "temporary network error",
				retryable: true,
			}
		}
		return nil // Success on third attempt
	}

	ctx := context.Background()
	start := time.Now()
	err := executor.Execute(ctx, operation)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected operation to be called 3 times, got: %d", callCount)
	}

	// Check that delays were applied (should be at least 2 delays of 10ms each)
	expectedMinDuration := 2 * 10 * time.Millisecond
	if duration < expectedMinDuration {
		t.Errorf("Expected duration of at least %v, got: %v", expectedMinDuration, duration)
	}
}

func TestRetryExecutor_RetryableErrorExceedsMaxRetries(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 2,
		Delay:      5 * time.Millisecond,
	}

	callCount := 0
	retryableErr := &testRetryableError{
		message:   "persistent network error",
		retryable: true,
	}

	operation := func() error {
		callCount++
		return retryableErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != retryableErr {
		t.Errorf("Expected last retry error, got: %v", err)
	}

	// Should call operation MaxRetries + 1 times (initial + retries)
	expectedCalls := executor.MaxRetries + 1
	if callCount != expectedCalls {
		t.Errorf("Expected operation to be called %d times, got: %d", expectedCalls, callCount)
	}
}

func TestRetryExecutor_NonRetryableError(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	nonRetryableErr := &testNonRetryableError{
		message: "validation error",
	}

	operation := func() error {
		callCount++
		return nonRetryableErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nonRetryableErr {
		t.Errorf("Expected non-retryable error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time only, got: %d", callCount)
	}
}

func TestRetryExecutor_RegularError(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	regularErr := errors.New("regular error")

	operation := func() error {
		callCount++
		return regularErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != regularErr {
		t.Errorf("Expected regular error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time only, got: %d", callCount)
	}
}

func TestRetryExecutor_RetryableErrorSetToFalse(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	nonRetryableErr := &testRetryableError{
		message:   "network error but not retryable",
		retryable: false,
	}

	operation := func() error {
		callCount++
		return nonRetryableErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nonRetryableErr {
		t.Errorf("Expected non-retryable error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time only, got: %d", callCount)
	}
}

func TestRetryExecutor_ContextCancellation(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 5,
		Delay:      50 * time.Millisecond, // Long delay to ensure cancellation
	}

	callCount := 0
	operation := func() error {
		callCount++
		return &testRetryableError{
			message:   "network timeout",
			retryable: true,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after first failure and during delay
	go func() {
		time.Sleep(25 * time.Millisecond) // Wait for first call and part of delay
		cancel()
	}()

	err := executor.Execute(ctx, operation)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	// Should have been called once before cancellation
	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time before cancellation, got: %d", callCount)
	}
}

func TestRetryExecutor_ContextTimeout(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 5,
		Delay:      20 * time.Millisecond,
	}

	callCount := 0
	operation := func() error {
		callCount++
		return &testRetryableError{
			message:   "network error",
			retryable: true,
		}
	}

	// Context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := executor.Execute(ctx, operation)

	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}

	// Should have been called at least once, but not all retries due to timeout
	if callCount < 1 {
		t.Errorf("Expected operation to be called at least 1 time, got: %d", callCount)
	}

	if callCount > 3 {
		t.Errorf("Expected operation to be called at most 3 times due to timeout, got: %d", callCount)
	}
}

func TestRetryExecutor_ZeroMaxRetries(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 0,
		Delay:      10 * time.Millisecond,
	}

	callCount := 0
	retryableErr := &testRetryableError{
		message:   "network error",
		retryable: true,
	}

	operation := func() error {
		callCount++
		return retryableErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != retryableErr {
		t.Errorf("Expected retryable error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time only with zero retries, got: %d", callCount)
	}
}

func TestRetryExecutor_MixedErrorTypes(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      5 * time.Millisecond,
	}

	callCount := 0
	finalErr := errors.New("final non-retryable error")

	operation := func() error {
		callCount++
		switch callCount {
		case 1:
			return &testRetryableError{message: "first retry error", retryable: true}
		case 2:
			return &testRetryableError{message: "second retry error", retryable: true}
		case 3:
			return finalErr // Non-retryable error
		default:
			return nil
		}
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != finalErr {
		t.Errorf("Expected final non-retryable error, got: %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected operation to be called 3 times, got: %d", callCount)
	}
}

func TestRetryExecutor_NetworkErrorType(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 2,
		Delay:      5 * time.Millisecond,
	}

	callCount := 0
	networkErr := &common.NetworkError{
		Operation:   "upload",
		Cause:       errors.New("connection timeout"),
		ShouldRetry: true,
	}

	operation := func() error {
		callCount++
		if callCount < 2 {
			return networkErr
		}
		return nil // Success on second attempt
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if callCount != 2 {
		t.Errorf("Expected operation to be called 2 times, got: %d", callCount)
	}
}

func TestRetryExecutor_NetworkErrorNonRetryable(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 3,
		Delay:      5 * time.Millisecond,
	}

	callCount := 0
	networkErr := &common.NetworkError{
		Operation:   "upload",
		Cause:       errors.New("authentication failed"),
		ShouldRetry: false,
	}

	operation := func() error {
		callCount++
		return networkErr
	}

	ctx := context.Background()
	err := executor.Execute(ctx, operation)

	if err != networkErr {
		t.Errorf("Expected network error, got: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected operation to be called 1 time only, got: %d", callCount)
	}
}

func TestRetryExecutor_DelayTimingAccuracy(t *testing.T) {
	executor := &common.RetryExecutor{
		MaxRetries: 2,
		Delay:      100 * time.Millisecond,
	}

	callCount := 0
	operation := func() error {
		callCount++
		return &testRetryableError{message: "temporary error", retryable: true}
	}

	ctx := context.Background()
	start := time.Now()
	err := executor.Execute(ctx, operation)
	duration := time.Since(start)

	// Should fail after MaxRetries + 1 calls with 2 delays
	if err == nil {
		t.Error("Expected error after max retries")
	}

	if callCount != 3 {
		t.Errorf("Expected 3 calls, got: %d", callCount)
	}

	// Check timing: should have 2 delays of 100ms each
	expectedMinDuration := 2 * 100 * time.Millisecond
	expectedMaxDuration := 250 * time.Millisecond // Allow some tolerance

	if duration < expectedMinDuration {
		t.Errorf("Duration too short: expected at least %v, got %v", expectedMinDuration, duration)
	}

	if duration > expectedMaxDuration {
		t.Errorf("Duration too long: expected at most %v, got %v", expectedMaxDuration, duration)
	}
}
