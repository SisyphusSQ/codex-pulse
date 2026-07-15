package runtimeclock

import "testing"

func TestCheckedRuntimeTimestampArithmetic(t *testing.T) {
	t.Parallel()

	if got, ok := Add(MaxTimestampMS-2, 2); !ok || got != MaxTimestampMS {
		t.Fatalf("Add() = %d, %v", got, ok)
	}
	if MaxContinuableTimestampMS+1 != MaxInProgressTimestampMS ||
		MaxInProgressTimestampMS+1 != MaxTimestampMS {
		t.Fatal("runtime state headroom constants are inconsistent")
	}
	for _, testCase := range []struct {
		name  string
		value int64
		delta int64
	}{
		{name: "negative value", value: -1, delta: 1},
		{name: "negative delta", value: 1, delta: -1},
		{name: "overflow", value: MaxTimestampMS, delta: 1},
	} {
		if _, ok := Add(testCase.value, testCase.delta); ok {
			t.Fatalf("Add(%s) succeeded", testCase.name)
		}
	}
	if got, ok := After(10, 10, MaxTimestampMS); !ok || got != 11 {
		t.Fatalf("After(equal) = %d, %v", got, ok)
	}
	if got, ok := After(12, 10, MaxTimestampMS); !ok || got != 12 {
		t.Fatalf("After(wall clock) = %d, %v", got, ok)
	}
	if _, ok := After(MaxTimestampMS, MaxTimestampMS-1, MaxTimestampMS-1); ok {
		t.Fatal("After(maximum) succeeded")
	}
}
