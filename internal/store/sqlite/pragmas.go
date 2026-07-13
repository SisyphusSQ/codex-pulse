package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SynchronousNormal is SQLite's numeric readback for PRAGMA synchronous=NORMAL.
const SynchronousNormal = 1

// ConnectionPragmas is the startup readback from one physical connection.
type ConnectionPragmas struct {
	JournalMode       string
	ForeignKeys       bool
	BusyTimeoutMillis int
	Synchronous       int
	QueryOnly         bool
}

// PragmaSnapshot records the validated writer and reader connection settings.
type PragmaSnapshot struct {
	Writer ConnectionPragmas
	Reader ConnectionPragmas
}

func readPragmas(ctx context.Context, database *sql.DB) (ConnectionPragmas, error) {
	connection, err := database.Conn(ctx)
	if err != nil {
		return ConnectionPragmas{}, classifyError("acquire pragma connection", err)
	}
	defer connection.Close()

	var pragmas ConnectionPragmas
	var foreignKeys int
	var queryOnly int
	queries := []struct {
		name   string
		target any
	}{
		{name: "journal_mode", target: &pragmas.JournalMode},
		{name: "foreign_keys", target: &foreignKeys},
		{name: "busy_timeout", target: &pragmas.BusyTimeoutMillis},
		{name: "synchronous", target: &pragmas.Synchronous},
		{name: "query_only", target: &queryOnly},
	}
	for _, query := range queries {
		if err := connection.QueryRowContext(ctx, "PRAGMA "+query.name).Scan(query.target); err != nil {
			return ConnectionPragmas{}, classifyError("read PRAGMA "+query.name, err)
		}
	}
	pragmas.JournalMode = strings.ToLower(pragmas.JournalMode)
	pragmas.ForeignKeys = foreignKeys == 1
	pragmas.QueryOnly = queryOnly == 1

	return pragmas, nil
}

func validatePragmas(role string, got ConnectionPragmas, config Config, queryOnly bool) error {
	wantBusyTimeout := int(config.BusyTimeout / time.Millisecond)
	switch {
	case got.JournalMode != "wal":
		return newClassifiedError("validate "+role+" pragmas", ErrInvalidConfig, fmt.Errorf("journal_mode=%q, want wal", got.JournalMode))
	case !got.ForeignKeys:
		return newClassifiedError("validate "+role+" pragmas", ErrInvalidConfig, fmt.Errorf("foreign_keys=off, want on"))
	case got.BusyTimeoutMillis != wantBusyTimeout:
		return newClassifiedError("validate "+role+" pragmas", ErrInvalidConfig, fmt.Errorf("busy_timeout=%d, want %d", got.BusyTimeoutMillis, wantBusyTimeout))
	case got.Synchronous != SynchronousNormal:
		return newClassifiedError("validate "+role+" pragmas", ErrInvalidConfig, fmt.Errorf("synchronous=%d, want %d", got.Synchronous, SynchronousNormal))
	case got.QueryOnly != queryOnly:
		return newClassifiedError("validate "+role+" pragmas", ErrInvalidConfig, fmt.Errorf("query_only=%t, want %t", got.QueryOnly, queryOnly))
	default:
		return nil
	}
}
