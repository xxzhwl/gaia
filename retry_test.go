package gaia

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetry tests the Retry function
func TestRetry(t *testing.T) {
	// Test case 1: Success on first try
	t.Run("SuccessOnFirstTry", func(t *testing.T) {
		count := 0
		err := Retry(func() error {
			count++
			return nil
		}, 3, 10*time.Millisecond)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 attempt, got %d", count)
		}
	})

	// Test case 2: Success after multiple tries
	t.Run("SuccessAfterMultipleTries", func(t *testing.T) {
		count := 0
		err := Retry(func() error {
			count++
			if count < 3 {
				return errors.New("temporary error")
			}
			return nil
		}, 5, 10*time.Millisecond)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if count != 3 {
			t.Errorf("Expected 3 attempts, got %d", count)
		}
	})

	// Test case 3: Failure after max retries
	t.Run("FailureAfterMaxRetries", func(t *testing.T) {
		count := 0
		err := Retry(func() error {
			count++
			return errors.New("persistent error")
		}, 3, 10*time.Millisecond)
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if count != 4 { // 3 retries + 1 initial attempt
			t.Errorf("Expected 4 attempts, got %d", count)
		}
	})

	// Test case 4: Zero retries
	t.Run("ZeroRetries", func(t *testing.T) {
		count := 0
		err := Retry(func() error {
			count++
			return errors.New("error")
		}, 0, 10*time.Millisecond)
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if count != 1 {
			t.Errorf("Expected 1 attempt, got %d", count)
		}
	})
}

// TestRunInterval tests the RunInterval function
func TestRunInterval(t *testing.T) {
	// Test case 1: Immediate execution
	t.Run("ImmediateExecution", func(t *testing.T) {
		count := 0
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := RunInterval(ctx, func() error {
			count++
			if count >= 3 {
				cancel()
			}
			return nil
		}, 20*time.Millisecond, true)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if count < 2 {
			t.Errorf("Expected at least 2 executions, got %d", count)
		}
	})

	// Test case 2: Delayed execution
	t.Run("DelayedExecution", func(t *testing.T) {
		count := 0
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := RunInterval(ctx, func() error {
			count++
			return nil
		}, 30*time.Millisecond, false)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		duration := time.Since(start)
		if duration < 30*time.Millisecond {
			t.Errorf("Expected delayed execution, but got %v", duration)
		}
	})

	// Test case 3: Context cancellation
	t.Run("ContextCancellation", func(t *testing.T) {
		count := 0
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()

		err := RunInterval(ctx, func() error {
			count++
			time.Sleep(10 * time.Millisecond)
			return nil
		}, 15*time.Millisecond, true)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if count < 2 {
			t.Errorf("Expected at least 2 executions before cancellation, got %d", count)
		}
	})

	// Test case 4: Error handling
	t.Run("ErrorHandling", func(t *testing.T) {
		count := 0
		expectedErr := errors.New("execution error")
		ctx := context.Background()

		err := RunInterval(ctx, func() error {
			count++
			return expectedErr
		}, 10*time.Millisecond, true)

		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
		if count != 1 {
			t.Errorf("Expected 1 execution, got %d", count)
		}
	})
}
