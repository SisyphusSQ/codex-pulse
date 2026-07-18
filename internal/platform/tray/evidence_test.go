package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderWritesDeterministicPrivateEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 2, 0, 0, 0, time.UTC)
	recorder, err := NewRecorder("darwin", "arm64", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	recorder.Observe("template_icon_configured", "19x19")
	recorder.SetWindowGeometry(WindowGeometry{Window: Rect{X: 10, Y: 20, Width: 360, Height: 240}, Screen: Rect{Width: 1512, Height: 982}, ScreenCount: 1, Primary: true})
	path := filepath.Join(t.TempDir(), "nested", "evidence.json")
	if err := recorder.Write(path); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if evidence.WailsVersion != LockedWailsVersion || evidence.GOOS != "darwin" || evidence.GOARCH != "arm64" {
		t.Fatalf("evidence platform/version = %#v", evidence)
	}
	if len(evidence.Observations) != 1 || evidence.Observations[0].OccurredAt != "2026-07-18T02:00:00Z" {
		t.Fatalf("observations = %#v", evidence.Observations)
	}
	if evidence.Geometry == nil || evidence.Geometry.Window.Width != 360 || evidence.Geometry.ScreenCount != 1 {
		t.Fatalf("geometry = %#v", evidence.Geometry)
	}
}
