package appserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRolloutPathUsesCompressedSiblingOnlyWhenPlainMissing(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".zst", []byte("metadata only"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, code := validatedRolloutPath(home, p); got != p+".zst" || code != "" {
		t.Fatal("compressed fallback missing", got, code)
	}
	if err := os.WriteFile(p, []byte("plain"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, code := validatedRolloutPath(home, p); got != p || code != "" {
		t.Fatal("plain preference changed", got, code)
	}
	if got, code := validatedRolloutPath(home, p+".zst"); got != p+".zst" || code != "" {
		t.Fatal("explicit compressed path rejected", got, code)
	}
	if _, code := validatedRolloutPath(home, filepath.Join(t.TempDir(), "outside.jsonl")); code != "rollout_path_outside_home" {
		t.Fatal("outside candidate probed", code)
	}
}
