package routine

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestGoRunsEveryFunction(t *testing.T) {
	var calls atomic.Int64
	fns := make([]func(), 100)
	for i := range fns {
		fns[i] = func() { calls.Add(1) }
	}
	if err := Go(8, fns); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	if calls.Load() != int64(len(fns)) {
		t.Fatalf("calls = %d, want %d", calls.Load(), len(fns))
	}
}

func TestGoEJoinsErrorsAndRejectsInvalidParallelism(t *testing.T) {
	errOne := errors.New("one")
	errTwo := errors.New("two")
	err := GoE(2, []func() error{
		func() error { return errOne },
		func() error { return nil },
		func() error { return errTwo },
	})
	if !errors.Is(err, errOne) || !errors.Is(err, errTwo) {
		t.Fatalf("GoE() error = %v", err)
	}
	if err = GoE(0, nil); err == nil {
		t.Fatal("GoE() expected invalid parallelism error")
	}
}
