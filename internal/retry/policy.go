package retry

import (
	"context"
	"errors"
	"math"
	"time"
)

var ErrInvalidPolicy = errors.New("retry policy is invalid")

type Config struct {
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	MaxAttempts int
	Jitter      func() float64
}

type Policy struct {
	baseDelay   time.Duration
	maxDelay    time.Duration
	maxAttempts int
	jitter      func() float64
}

func NewPolicy(config Config) (Policy, error) {
	if config.BaseDelay <= 0 || config.MaxDelay < config.BaseDelay ||
		config.MaxAttempts < 1 || config.MaxAttempts > 100 || config.Jitter == nil {
		return Policy{}, ErrInvalidPolicy
	}
	return Policy{
		baseDelay: config.BaseDelay, maxDelay: config.MaxDelay,
		maxAttempts: config.MaxAttempts, jitter: config.Jitter,
	}, nil
}

// Delay 返回第 attempt 次失败后的退避。指数项和最终结果都受 MaxDelay 限制，
// jitter source 必须位于 [0,1)，对应指数项的 [0,50%) 抖动。
func (policy Policy) Delay(attempt int) (time.Duration, bool, error) {
	if attempt < 1 || policy.baseDelay <= 0 || policy.maxDelay < policy.baseDelay ||
		policy.maxAttempts < 1 || policy.jitter == nil {
		return 0, false, ErrInvalidPolicy
	}
	if attempt > policy.maxAttempts {
		return 0, false, nil
	}
	exponential := policy.baseDelay
	for current := 1; current < attempt; current++ {
		if exponential >= policy.maxDelay || exponential > policy.maxDelay/2 {
			exponential = policy.maxDelay
			break
		}
		exponential *= 2
	}
	sample := policy.jitter()
	if math.IsNaN(sample) || math.IsInf(sample, 0) || sample < 0 || sample >= 1 {
		return 0, false, ErrInvalidPolicy
	}
	jitter := time.Duration(float64(exponential) * 0.5 * sample)
	if jitter > policy.maxDelay-exponential {
		return policy.maxDelay, true, nil
	}
	return exponential + jitter, true, nil
}

func Wait(ctx context.Context, delay time.Duration) error {
	if ctx == nil || delay <= 0 {
		return ErrInvalidPolicy
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
