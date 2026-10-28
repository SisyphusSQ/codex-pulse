//go:build windows

package preferences

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsPreferencesCASAndLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "私有 preferences", "preferences.json")
	first, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := NewCurrentSnapshot(nil, DefaultOnlinePreferences())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.InitializePreferences(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	second, _ := NewFileStore(path)
	current, err := second.LoadPreferences(t.Context())
	if err != nil || current.Revision != 1 {
		t.Fatalf("read initial = %#v, %v", current, err)
	}
	current.Revision++
	current.UI.Locale = LocaleEnglishUS
	if err := first.CompareAndSwap(t.Context(), 1, current); err != nil {
		t.Fatal(err)
	}
	if err := second.CompareAndSwap(t.Context(), 1, current); !errors.Is(err, ErrPreferencesConflict) {
		t.Fatalf("stale CAS = %v", err)
	}
	readback, err := second.LoadPreferences(t.Context())
	if err != nil || readback.Revision != 2 || readback.UI.Locale != LocaleEnglishUS {
		t.Fatalf("readback = %#v, %v", readback, err)
	}
	lease, err := first.AcquireSwitchLease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if other, err := second.AcquireSwitchLease(ctx); !errors.Is(err, context.DeadlineExceeded) || other != nil {
		t.Fatalf("competing switch = %v, %v", other, err)
	}
	lease.Release()
	lease, err = second.AcquireSwitchLease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
}
