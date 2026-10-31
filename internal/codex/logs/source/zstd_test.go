package source

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compressedFixture(t *testing.T, content []byte) (Snapshot, *SnapshotReader) {
	t.Helper()
	home, discoverer := confirmedInspectFixture(t)
	p := filepath.Join(home, "sessions", "compressed.jsonl.zst")
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := encoder.EncodeAll(content, nil)
	encoder.Close()
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := discoverer.Inspect(t.Context(), p, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewSnapshotReader(home, 13)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, reader
}

func TestZstdSnapshotLogicalOffsetsCloseAndIdentity(t *testing.T) {
	content := bytes.Repeat([]byte("{\"type\":\"event_msg\"}\n"), 300)
	physical, reader := compressedFixture(t, content)
	view, logical, err := reader.OpenZstdSnapshot(t.Context(), physical)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if logical.Fingerprint.SizeBytes != int64(len(content)) || validateSnapshot(logical) != nil {
		t.Fatal("invalid logical snapshot")
	}
	var got []byte
	offset, err := view.Read(t.Context(), logical, 7, func(b []byte, _ bool) error { got = append(got, b...); return nil })
	if err != nil || offset != int64(len(content)) || !bytes.Equal(got, content[7:]) {
		t.Fatal("logical resume failed", err)
	}
	if err := os.WriteFile(physical.Path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Read(t.Context(), logical, 0, func([]byte, bool) error { t.Fatal("changed source delivered bytes"); return nil }); !errors.Is(err, ErrChangedDuringScan) {
		t.Fatal("identity fence lost", err)
	}
	view.Close()
	if len(view.decoded.data) != 0 {
		t.Fatal("decoded body retained")
	}
}

func TestZstdSnapshotRejectsCorruptionBudgetsAndCancellation(t *testing.T) {
	physical, reader := compressedFixture(t, bytes.Repeat([]byte("x"), 4096))
	if _, _, err := reader.openZstdSnapshot(t.Context(), physical, physical.Fingerprint.SizeBytes-1, 8192); !errors.Is(err, ErrInvalidCompressedSource) {
		t.Fatal("compressed budget ignored", err)
	}
	if _, _, err := reader.openZstdSnapshot(t.Context(), physical, 8192, 1024); !errors.Is(err, ErrInvalidCompressedSource) {
		t.Fatal("decoded budget ignored", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := reader.OpenZstdSnapshot(ctx, physical); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	data, err := os.ReadFile(physical.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(physical.Path, data[:len(data)-1], 0600); err != nil {
		t.Fatal(err)
	}
	discoverer, err := NewDiscoverer(reader.home)
	if err != nil {
		t.Fatal(err)
	}
	physical, err = discoverer.Inspect(t.Context(), physical.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.OpenZstdSnapshot(t.Context(), physical); !errors.Is(err, ErrInvalidCompressedSource) {
		t.Fatal("truncated frame accepted", err)
	}
}
