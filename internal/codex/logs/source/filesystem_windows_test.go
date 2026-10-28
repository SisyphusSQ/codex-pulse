//go:build windows

package source

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
)

func TestWindowsHomeDiscoverySnapshotAndIncrement(t *testing.T) {
	home := filepath.Join(t.TempDir(), "中文 Codex Home")
	path := writeJSONLFixture(t, home, "sessions/2026/10/session.jsonl", "first\n")
	metadata, err := NewHomeProbe().Probe(t.Context(), home)
	if err != nil || metadata.JSONLFiles != 1 || metadata.JSONLBytes != 6 || !homeidentity.IsStableDeviceID(metadata.DeviceID) || metadata.Inode != 1 {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}
	discoverer, err := NewConfirmedDiscoverer(home, metadata.DeviceID, metadata.Inode)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := discoverer.Discover(t.Context())
	if err != nil || len(initial.Snapshots) != 1 {
		t.Fatalf("discovery = %#v, %v", initial, err)
	}
	reader, err := NewConfirmedSnapshotReader(home, metadata.DeviceID, metadata.Inode, 2)
	if err != nil {
		t.Fatal(err)
	}
	var content bytes.Buffer
	offset, err := reader.Read(t.Context(), initial.Snapshots[0], 0, func(chunk []byte, _ bool) error { _, err := content.Write(chunk); return err })
	if err != nil || offset != 6 || content.String() != "first\n" {
		t.Fatalf("snapshot = %q at %d, %v", content.String(), offset, err)
	}
	appendFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendFile.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	appendFile.Close()
	grown, err := discoverer.Discover(t.Context())
	if err != nil || len(grown.Snapshots) != 1 || grown.Snapshots[0].SourceFileID != initial.Snapshots[0].SourceFileID {
		t.Fatalf("increment = %#v, %v", grown, err)
	}
	content.Reset()
	if _, err := reader.Read(t.Context(), grown.Snapshots[0], offset, func(chunk []byte, _ bool) error { _, err := content.Write(chunk); return err }); err != nil || content.String() != "second\n" {
		t.Fatalf("incremental read = %q, %v", content.String(), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.Read(ctx, grown.Snapshots[0], 0, func([]byte, bool) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if err := os.Rename(home, home+"-old"); err != nil {
		t.Fatal(err)
	}
	writeJSONLFixture(t, home, "sessions/replaced.jsonl", "replacement\n")
	if _, err := discoverer.Discover(t.Context()); !errors.Is(err, ErrHomeChanged) {
		t.Fatalf("replaced Home = %v", err)
	}
}

func TestWindowsSnapshotRejectsConcurrentChangeAndSupportsLongPaths(t *testing.T) {
	home := t.TempDir()
	relative := "sessions/" + strings.Repeat("long-directory/", 22) + "session.jsonl"
	path := writeJSONLFixture(t, home, relative, "before\n")
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatal(err)
	}
	result, err := discoverer.Discover(t.Context())
	if err != nil || len(result.Snapshots) != 1 {
		t.Fatalf("long path discovery = %#v, %v", result, err)
	}
	reader, err := NewSnapshotReader(home, 2)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	_, err = reader.Read(t.Context(), result.Snapshots[0], 0, func([]byte, bool) error {
		if !changed {
			changed = true
			return os.WriteFile(path, []byte("changed\n"), 0o600)
		}
		return nil
	})
	if !errors.Is(err, ErrChangedDuringScan) {
		t.Fatalf("concurrent change = %v", err)
	}
}
