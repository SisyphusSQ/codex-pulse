package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestOptionalReportingStartsDisabledAndClosesWithApplication(t *testing.T) {
	// All provider probes use an empty synthetic user Home, never personal Agent data.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "missing-codex"))
	dir := filepath.Join(home, "private-runtime")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	broker, err := core.NewInvalidationBroker(16)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Open(t.Context(), Config{Broker: broker, Store: storesqlite.Config{Path: filepath.Join(dir, "app.db")}, PreferencesPath: filepath.Join(dir, "preferences.json"), DefaultCodexHome: filepath.Join(home, "missing-codex")})
	if err != nil {
		t.Fatal(err)
	}
	status, err := runtime.Service().ReportingStatus(t.Context())
	if err != nil || status.Enabled || status.State != "disabled" || status.Endpoint != "" {
		t.Fatalf("status: %+v %v", status, err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "reporting.db"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("reporting state missing or unsafe")
	}
}
