package sqlite

import (
	"context"
	"errors"
	"fmt"

	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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

	var driverError *modernsqlite.Error
	if errors.As(err, &driverError) {
		var kind error
		switch driverError.Code() & 0xff {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			kind = ErrBusy
		case sqlite3.SQLITE_FULL:
			kind = ErrDiskFull
		case sqlite3.SQLITE_READONLY:
			kind = ErrReadOnly
		case sqlite3.SQLITE_PERM, sqlite3.SQLITE_CANTOPEN:
			kind = ErrPermission
		case sqlite3.SQLITE_IOERR:
			kind = ErrIO
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			kind = ErrCorrupt
		case sqlite3.SQLITE_INTERRUPT:
			kind = ErrCanceled
		}
		if kind != nil {
			return newClassifiedError(op, kind, err)
		}
	}

	return fmt.Errorf("sqlite %s: %w", op, err)
}
