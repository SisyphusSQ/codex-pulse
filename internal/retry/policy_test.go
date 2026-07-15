package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPolicyReturnsBoundedExponentialDelayWithInjectedJitter(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		MaxAttempts: 5,
		Jitter:      func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	want := []time.Duration{
		1250 * time.Millisecond,
		2500 * time.Millisecond,
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
	}
	for index, expected := range want {
		delay, retry, err := policy.Delay(index + 1)
		if err != nil || !retry || delay != expected {
			t.Fatalf("Delay(%d) = %s, %t, %v; want %s, true", index+1, delay, retry, err, expected)
		}
	}
	if delay, retry, err := policy.Delay(6); err != nil || retry || delay != 0 {
		t.Fatalf("Delay(exhausted) = %s, %t, %v; want zero, false", delay, retry, err)
	}
}

func TestPolicyCapsDelayAndRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	policy, err := NewPolicy(Config{
		BaseDelay:   20 * time.Second,
		MaxDelay:    30 * time.Second,
		MaxAttempts: 3,
		Jitter:      func() float64 { return 0.99 },
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		delay, retry, err := policy.Delay(attempt)
		if err != nil || !retry || delay > 30*time.Second {
			t.Fatalf("Delay(%d) = %s, %t, %v; want <= 30s", attempt, delay, retry, err)
		}
	}
	if _, _, err := policy.Delay(0); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Delay(0) error = %v, want ErrInvalidPolicy", err)
	}
	for _, config := range []Config{
		{},
		{BaseDelay: time.Second, MaxDelay: time.Second, MaxAttempts: 1, Jitter: nil},
		{BaseDelay: 2 * time.Second, MaxDelay: time.Second, MaxAttempts: 1, Jitter: func() float64 { return 0 }},
	} {
		if _, err := NewPolicy(config); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("NewPolicy(%#v) error = %v, want ErrInvalidPolicy", config, err)
		}
	}
}

func TestWaitIsCancelable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait(cancelled) error = %v, want context.Canceled", err)
	}
	if err := Wait(context.Background(), 0); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Wait(zero) error = %v, want ErrInvalidPolicy", err)
	}
}
