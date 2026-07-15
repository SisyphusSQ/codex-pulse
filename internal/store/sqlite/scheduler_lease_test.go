package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSchedulerOwnerLeaseSerializesAndReleases(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, Config{})
	first, err := store.AcquireSchedulerOwnerLease(context.Background())
	if err != nil {
		t.Fatalf("AcquireSchedulerOwnerLease(first) error = %v", err)
	}
	t.Cleanup(first.Release)

	if lease, err := store.TryAcquireSchedulerOwnerLease(context.Background()); !errors.Is(err, ErrOwnerLeaseBusy) || lease != nil {
		t.Fatalf("TryAcquireSchedulerOwnerLease(blocked) = %#v, %v, want ErrOwnerLeaseBusy", lease, err)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelWait()
	if lease, err := store.AcquireSchedulerOwnerLease(waitCtx); !errors.Is(err, context.DeadlineExceeded) || lease != nil {
		t.Fatalf("AcquireSchedulerOwnerLease(blocked) = %#v, %v, want deadline", lease, err)
	}

	first.Release()
	second, err := store.TryAcquireSchedulerOwnerLease(context.Background())
	if err != nil {
		t.Fatalf("TryAcquireSchedulerOwnerLease(after release) error = %v", err)
	}
	second.Release()
	info, err := os.Lstat(store.Config().Path + schedulerOwnerLockSuffix)
	if err != nil {
		t.Fatalf("Lstat(scheduler owner lock) error = %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("scheduler owner lock mode = %v, want regular 0600", info.Mode())
	}
}

func TestSchedulerOwnerLeaseRejectsUnsafeLockFile(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, lockPath string) {
				t.Helper()
				target := filepath.Join(t.TempDir(), "lock-target")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatalf("WriteFile(lock target) error = %v", err)
				}
				if err := os.Symlink(target, lockPath); err != nil {
					t.Fatalf("Symlink(lock) error = %v", err)
				}
			},
		},
		{
			name: "broad mode",
			setup: func(t *testing.T, lockPath string) {
				t.Helper()
				if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
					t.Fatalf("WriteFile(lock) error = %v", err)
				}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := openTestStore(t, Config{})
			lockPath := store.Config().Path + schedulerOwnerLockSuffix
			test.setup(t, lockPath)
			if lease, err := store.AcquireSchedulerOwnerLease(context.Background()); !errors.Is(err, ErrPermission) || lease != nil {
				t.Fatalf("AcquireSchedulerOwnerLease(unsafe lock) = %#v, %v, want ErrPermission", lease, err)
			}
		})
	}
}
