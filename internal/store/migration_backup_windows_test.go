//go:build windows

package store

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestWindowsMigrationBackupSpaceAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.db")
	if err := ensureMigrationBackupSpace(t.Context(), path, math.MaxInt64); !errors.Is(err, storesqlite.ErrDiskFull) {
		t.Fatalf("insufficient backup space = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ensureMigrationBackupSpace(ctx, path, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel backup space query = %v", err)
	}
}
