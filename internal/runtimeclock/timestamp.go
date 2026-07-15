// Package runtimeclock defines the shared persistence timestamp boundary and
// checked logical-time arithmetic used by runtime state machines.
package runtimeclock

const (
	MaxTimestampMS            int64 = 253_402_300_799_999
	MaxInProgressTimestampMS        = MaxTimestampMS - 1
	MaxContinuableTimestampMS       = MaxTimestampMS - 2
)

// Add returns value+delta only when both inputs and the result stay inside the
// shared non-negative runtime timestamp domain.
func Add(value int64, delta int64) (int64, bool) {
	if value < 0 || delta < 0 || value > MaxTimestampMS || delta > MaxTimestampMS-value {
		return 0, false
	}
	return value + delta, true
}

// Successor returns the next representable runtime millisecond.
func Successor(value int64) (int64, bool) {
	return Add(value, 1)
}

// After returns a timestamp strictly after minimum, preferring wall-clock now,
// without exceeding maximum. minimum=-1 is accepted for zero-based admission.
func After(now int64, minimum int64, maximum int64) (int64, bool) {
	if now < 0 || minimum < -1 || maximum < 0 || maximum > MaxTimestampMS ||
		now > maximum || minimum >= maximum {
		return 0, false
	}
	if now > minimum {
		return now, true
	}
	next, ok := Successor(minimum)
	return next, ok && next <= maximum
}
