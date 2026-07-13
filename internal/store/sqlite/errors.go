package sqlite

import (
	"context"
	"errors"
	"fmt"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// Stable errors let callers branch on operational failures without matching
// driver-specific strings. Wrapped context and driver errors remain available
// through errors.Is and errors.As.
var (
	ErrInvalidConfig = errors.New("invalid sqlite store configuration")
	ErrInvalidPath   = errors.New("invalid sqlite store path")
	ErrQueueFull     = errors.New("sqlite write queue is full")
	ErrClosing       = errors.New("sqlite store is closing")
	ErrClosed        = errors.New("sqlite store is closed")
	ErrCanceled      = errors.New("sqlite operation canceled")
	ErrBusy          = errors.New("sqlite database is busy")
	ErrDiskFull      = errors.New("sqlite database disk is full")
	ErrReadOnly      = errors.New("sqlite database is read-only")
	ErrPermission    = errors.New("sqlite database permission denied")
	ErrIO            = errors.New("sqlite database I/O failure")
	ErrCorrupt       = errors.New("sqlite database is corrupt")
	ErrCallbackPanic = errors.New("sqlite write callback panicked")
)

type classifiedError struct {
	op    string
	kind  error
	cause error
}

func (err *classifiedError) Error() string {
	if err.cause == nil {
		return fmt.Sprintf("sqlite %s: %v", err.op, err.kind)
	}
	return fmt.Sprintf("sqlite %s: %v: %v", err.op, err.kind, err.cause)
}

func (err *classifiedError) Unwrap() []error {
	if err.cause == nil {
		return []error{err.kind}
	}
	return []error{err.kind, err.cause}
}

func newClassifiedError(op string, kind, cause error) error {
	return &classifiedError{op: op, kind: kind, cause: cause}
}

func classifyError(op string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return newClassifiedError(op, ErrCanceled, err)
	}

	var driverError sqlite3.Error
	if errors.As(err, &driverError) {
		var kind error
		switch driverError.Code {
		case sqlite3.ErrBusy, sqlite3.ErrLocked:
			kind = ErrBusy
		case sqlite3.ErrFull:
			kind = ErrDiskFull
		case sqlite3.ErrReadonly:
			kind = ErrReadOnly
		case sqlite3.ErrPerm, sqlite3.ErrCantOpen:
			kind = ErrPermission
		case sqlite3.ErrIoErr:
			kind = ErrIO
		case sqlite3.ErrCorrupt, sqlite3.ErrNotADB:
			kind = ErrCorrupt
		case sqlite3.ErrInterrupt:
			kind = ErrCanceled
		}
		if kind != nil {
			return newClassifiedError(op, kind, err)
		}
	}

	return fmt.Errorf("sqlite %s: %w", op, err)
}
