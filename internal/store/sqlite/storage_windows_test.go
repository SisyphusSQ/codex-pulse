//go:build windows

package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsWALBackupRestoreAndOwnerLease(t *testing.T) {
	store := openTestStore(t, Config{})
	createEventsTable(t, store)
	if err := insertEvent(store, t.Context(), "committed-before-backup"); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireSchedulerOwnerLease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(t.Context(), store.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(context.Background())
	if competing, err := other.TryAcquireSchedulerOwnerLease(t.Context()); competing != nil || !errors.Is(err, ErrOwnerLeaseBusy) {
		t.Fatalf("competing writer = %v, %v", competing, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, err := other.AcquireSchedulerOwnerLease(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel owner wait = %v", err)
	}
	lease.Release()
	lease, err = other.TryAcquireSchedulerOwnerLease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	backup := filepath.Join(filepath.Dir(store.Config().Path), "backups", "checkpoint.db")
	if _, err := store.Backup(t.Context(), BackupOptions{Destination: backup}); err != nil {
		t.Fatal(err)
	}
	if err := insertEvent(store, t.Context(), "after-backup"); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(filepath.Dir(store.Config().Path), "restored.db")
	if _, err := Restore(t.Context(), RestoreOptions{Source: backup, Destination: restored}); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, Config{Path: restored})
	values := readEventValues(t, reopened)
	if len(values) != 1 || values[0] != "committed-before-backup" {
		t.Fatalf("restored values = %v", values)
	}
}
