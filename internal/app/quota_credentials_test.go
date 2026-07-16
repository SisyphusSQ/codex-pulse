package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestAuthFileCredentialProviderLeasesCurrentHomeTokenAndClearsCopy(t *testing.T) {
	t.Parallel()

	homeA := writeSyntheticAuthHome(t, "synthetic-access-token-a")
	homeB := writeSyntheticAuthHome(t, "synthetic-access-token-b")
	loader := &quotaRuntimePreferencesLoader{snapshot: quotaRuntimePreferencesForHome(t, homeA)}
	provider, err := newAuthFileCredentialProvider(loader)
	if err != nil {
		t.Fatalf("newAuthFileCredentialProvider() error = %v", err)
	}

	var firstLease []byte
	callbackError := errors.New("synthetic callback stopped")
	err = provider.WithAccessToken(context.Background(), func(token []byte) error {
		firstLease = token
		if string(token) != "synthetic-access-token-a" {
			t.Fatalf("first token = %q", token)
		}
		return callbackError
	})
	if !errors.Is(err, callbackError) {
		t.Fatalf("WithAccessToken(first) error = %v, want callback error", err)
	}
	if !allZeroBytes(firstLease) {
		t.Fatalf("first lease retained token bytes after callback")
	}

	loader.setSnapshot(quotaRuntimePreferencesForHome(t, homeB))
	var secondLease []byte
	if err := provider.WithAccessToken(context.Background(), func(token []byte) error {
		secondLease = token
		if string(token) != "synthetic-access-token-b" {
			t.Fatalf("second token = %q", token)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithAccessToken(second) error = %v", err)
	}
	if !allZeroBytes(secondLease) {
		t.Fatalf("second lease retained token bytes after callback")
	}
}

func TestAuthFileCredentialProviderFailsClosedWithoutLeakingContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(testing.TB) (string, string)
	}{
		{
			name: "missing auth file",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				return t.TempDir(), ""
			},
		},
		{
			name: "missing tokens object",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				return writeSyntheticAuthContent(t, []byte(`{"auth_mode":"chatgpt"}`)), ""
			},
		},
		{
			name: "duplicate access token key",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				marker := "synthetic-duplicate-token-marker"
				return writeSyntheticAuthContent(t, []byte(`{"tokens":{"access_token":"`+marker+`","access_token":"other"}}`)), marker
			},
		},
		{
			name: "escaped duplicate access token key",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				marker := "synthetic-escaped-duplicate-marker"
				return writeSyntheticAuthContent(t, []byte(`{"tokens":{"access_token":"`+marker+`","\u0061ccess_token":"other"}}`)), marker
			},
		},
		{
			name: "duplicate nested unknown key",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				marker := "synthetic-nested-duplicate-marker"
				return writeSyntheticAuthContent(t, []byte(`{"tokens":{"access_token":"valid","private":{"marker":"`+marker+`","marker":"other"}}}`)), marker
			},
		},
		{
			name: "empty access token",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				return writeSyntheticAuthContent(t, []byte(`{"tokens":{"access_token":"  "}}`)), ""
			},
		},
		{
			name: "auth path is directory",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				home := t.TempDir()
				if err := os.Mkdir(filepath.Join(home, "auth.json"), 0o700); err != nil {
					t.Fatalf("os.Mkdir(auth.json) error = %v", err)
				}
				return home, ""
			},
		},
		{
			name: "auth path is symlink",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				home := t.TempDir()
				target := filepath.Join(t.TempDir(), "outside.json")
				if err := os.WriteFile(target, []byte(`{"tokens":{"access_token":"synthetic-symlink-marker"}}`), 0o600); err != nil {
					t.Fatalf("os.WriteFile(outside) error = %v", err)
				}
				if err := os.Symlink(target, filepath.Join(home, "auth.json")); err != nil {
					t.Fatalf("os.Symlink(auth.json) error = %v", err)
				}
				return home, "synthetic-symlink-marker"
			},
		},
		{
			name: "auth file exceeds limit",
			setup: func(t testing.TB) (string, string) {
				t.Helper()
				marker := "synthetic-oversize-token-marker"
				content := append([]byte(`{"tokens":{"access_token":"`+marker+`","padding":"`), bytes.Repeat([]byte("x"), 2<<20)...)
				content = append(content, []byte(`"}}`)...)
				return writeSyntheticAuthContent(t, content), marker
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home, marker := test.setup(t)
			loader := &quotaRuntimePreferencesLoader{snapshot: quotaRuntimePreferencesForHome(t, home)}
			provider, err := newAuthFileCredentialProvider(loader)
			if err != nil {
				t.Fatalf("newAuthFileCredentialProvider() error = %v", err)
			}
			called := false
			err = provider.WithAccessToken(context.Background(), func([]byte) error {
				called = true
				return nil
			})
			if !errors.Is(err, quotaonline.ErrCredentialUnavailable) {
				t.Fatalf("WithAccessToken() error = %v, want ErrCredentialUnavailable", err)
			}
			if called {
				t.Fatalf("callback ran for invalid auth file")
			}
			if marker != "" && strings.Contains(err.Error(), marker) {
				t.Fatalf("error leaked auth marker")
			}
		})
	}
}

func TestAuthFileCredentialProviderRejectsChangedHomeAndCancellation(t *testing.T) {
	t.Parallel()

	home := writeSyntheticAuthHome(t, "synthetic-access-token")
	snapshot := quotaRuntimePreferencesForHome(t, home)
	snapshot.CodexHome.Source.Inode++
	provider, err := newAuthFileCredentialProvider(&quotaRuntimePreferencesLoader{snapshot: snapshot})
	if err != nil {
		t.Fatalf("newAuthFileCredentialProvider() error = %v", err)
	}
	if err := provider.WithAccessToken(context.Background(), func([]byte) error { return nil }); !errors.Is(err, quotaonline.ErrCredentialUnavailable) {
		t.Fatalf("WithAccessToken(changed home) error = %v", err)
	}

	validProvider, err := newAuthFileCredentialProvider(&quotaRuntimePreferencesLoader{
		snapshot: quotaRuntimePreferencesForHome(t, home),
	})
	if err != nil {
		t.Fatalf("newAuthFileCredentialProvider(valid) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := validProvider.WithAccessToken(ctx, func([]byte) error {
		called = true
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("WithAccessToken(cancelled) error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatalf("callback ran after cancellation")
	}
}

func TestAuthFileCredentialProviderRejectsHomeSwitchDuringLease(t *testing.T) {
	t.Parallel()

	homeA := writeSyntheticAuthHome(t, "synthetic-stale-home-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-current-home-token")
	loader := &quotaRuntimeSequencePreferencesLoader{snapshots: []preferences.Snapshot{
		quotaRuntimePreferencesForHome(t, homeA),
		quotaRuntimePreferencesForHome(t, homeB),
	}}
	provider, err := newAuthFileCredentialProvider(loader)
	if err != nil {
		t.Fatalf("newAuthFileCredentialProvider() error = %v", err)
	}
	called := false
	err = provider.WithAccessToken(context.Background(), func([]byte) error {
		called = true
		return nil
	})
	if !errors.Is(err, quotaonline.ErrCredentialUnavailable) {
		t.Fatalf("WithAccessToken() error = %v, want ErrCredentialUnavailable", err)
	}
	if called {
		t.Fatal("callback received a credential from the stale Home")
	}
}

func TestAuthFileCredentialProviderRejectsAuthReplacementDuringRead(t *testing.T) {
	t.Parallel()

	home := writeSyntheticAuthHome(t, "synthetic-replaced-old-token-marker")
	replacement := filepath.Join(home, "auth.replacement")
	if err := os.WriteFile(
		replacement,
		[]byte(`{"tokens":{"access_token":"synthetic-replaced-new-token-marker"}}`),
		0o600,
	); err != nil {
		t.Fatalf("os.WriteFile(replacement) error = %v", err)
	}
	loader := &quotaRuntimePreferencesLoader{snapshot: quotaRuntimePreferencesForHome(t, home)}
	provider, err := newAuthFileCredentialProvider(loader)
	if err != nil {
		t.Fatalf("newAuthFileCredentialProvider() error = %v", err)
	}
	provider.readAuthFile = func(
		ctx context.Context,
		source preferences.ConfirmedSource,
	) ([]byte, error) {
		return readConfirmedAuthFileWithHooks(ctx, source, authFileReadHooks{
			afterRead: func() {
				if renameErr := os.Rename(replacement, filepath.Join(home, "auth.json")); renameErr != nil {
					t.Fatalf("os.Rename(replacement) error = %v", renameErr)
				}
			},
		})
	}
	called := false
	err = provider.WithAccessToken(context.Background(), func([]byte) error {
		called = true
		return nil
	})
	if !errors.Is(err, quotaonline.ErrCredentialUnavailable) {
		t.Fatalf("WithAccessToken(replaced auth) error = %v", err)
	}
	if called {
		t.Fatal("callback received a credential across auth replacement")
	}
	for _, marker := range []string{
		"synthetic-replaced-old-token-marker",
		"synthetic-replaced-new-token-marker",
	} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("error leaked replacement marker %q", marker)
		}
	}
}

type quotaRuntimePreferencesLoader struct {
	mu       sync.RWMutex
	snapshot preferences.Snapshot
	err      error
}

type quotaRuntimeSequencePreferencesLoader struct {
	mu        sync.Mutex
	snapshots []preferences.Snapshot
	next      int
}

func (loader *quotaRuntimeSequencePreferencesLoader) LoadPreferences(
	ctx context.Context,
) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if len(loader.snapshots) == 0 {
		return preferences.Snapshot{}, errors.New("no synthetic snapshot")
	}
	index := loader.next
	if index >= len(loader.snapshots) {
		index = len(loader.snapshots) - 1
	}
	loader.next++
	return loader.snapshots[index], nil
}

func (loader *quotaRuntimePreferencesLoader) LoadPreferences(ctx context.Context) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.RLock()
	defer loader.mu.RUnlock()
	return loader.snapshot, loader.err
}

func (loader *quotaRuntimePreferencesLoader) setSnapshot(snapshot preferences.Snapshot) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.snapshot = snapshot
}

func writeSyntheticAuthHome(t testing.TB, accessToken string) string {
	t.Helper()
	return writeSyntheticAuthContent(t, []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"synthetic-id","access_token":"`+accessToken+`","refresh_token":"synthetic-refresh","account_id":"synthetic-account"}}`))
}

func writeSyntheticAuthContent(t testing.TB, content []byte) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth.json) error = %v", err)
	}
	return home
}

func quotaRuntimePreferencesForHome(t testing.TB, home string) preferences.Snapshot {
	t.Helper()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(canonicalHome, &stat); err != nil {
		t.Fatalf("unix.Stat(home) error = %v", err)
	}
	return preferences.Snapshot{CodexHome: preferences.CodexHomePreferences{
		Source: preferences.ConfirmedSource{
			Path: filepath.Clean(canonicalHome), DeviceID: strconv.FormatUint(uint64(uint32(stat.Dev)), 10),
			Inode:         int64(stat.Ino),
			ConfirmedAtMS: 1_784_000_000_000,
		},
		Generation: 1, DataStoreKey: preferences.DefaultDataStoreKey,
	}}
}

func allZeroBytes(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
