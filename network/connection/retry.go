package connection

import (
	"context"
	"time"
)

type RetryConfig struct {
	MaxAttempts       int
	InitialDelay      time.Duration
	MaxDelay          time.Duration
	BackoffMultiplier float64
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       3,
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          2 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

func WithRetry[T any](ctx context.Context, operation func(context.Context) (T, error), config RetryConfig) (T, error) {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}

		result, err := operation(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt < config.MaxAttempts-1 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				var zero T
				return zero, ctx.Err()
			case <-timer.C:
			}

			delay *= time.Duration(config.BackoffMultiplier)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}

	var zero T
	return zero, lastErr
}
