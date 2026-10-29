//go:build windows

package cursorpaths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWindowsLocationsRespectRoamingAndIsolatedOverrides(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "中文 Profile")
	roaming := filepath.Join(t.TempDir(), "独立 Roaming")
	t.Setenv("USERPROFILE", profile)
	t.Setenv("APPDATA", roaming)
	t.Setenv("CODEX_PULSE_CURSOR_HOME", "")
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", "")
	paths, err := Resolve()
	if err != nil || paths.StateDatabase != filepath.Join(roaming, "Cursor", "User", "globalStorage", "state.vscdb") || paths.ProjectsRoot != filepath.Join(profile, ".cursor", "projects") {
		t.Fatalf("Windows locations: %v", err)
	}
	isolate := filepath.Join(t.TempDir(), "测试 Home")
	t.Setenv("CODEX_PULSE_CURSOR_HOME", isolate)
	paths, err = Resolve()
	if err != nil || paths.StateDatabase != filepath.Join(isolate, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb") || paths.AITrackingDatabase != filepath.Join(isolate, ".cursor", "ai-tracking", "ai-code-tracking.db") {
		t.Fatalf("isolated override fell through: %v", err)
	}
	portable := filepath.Join(t.TempDir(), "Portable data", "user-data")
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", portable)
	paths, err = Resolve()
	if err != nil || paths.ConversationDatabase != filepath.Join(portable, "User", "globalStorage", "conversation-search.db") {
		t.Fatalf("custom user data: %v", err)
	}
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", `relative\user-data`)
	if _, err := Resolve(); !errors.Is(err, ErrPaths) {
		t.Fatal("relative user-data override accepted")
	}
	t.Setenv("CODEX_PULSE_CURSOR_USER_DATA_DIR", "")
	t.Setenv("CODEX_PULSE_CURSOR_HOME", `C:relative`)
	if _, err := Resolve(); !errors.Is(err, ErrPaths) {
		t.Fatal("drive-relative profile override accepted")
	}
}
