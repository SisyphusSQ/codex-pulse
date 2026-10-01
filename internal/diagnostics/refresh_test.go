package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefreshDiagnosticsArePrivateBoundedAndKeepFirstFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	logger, err := Open(root, "0.14.3")
	if err != nil {
		t.Fatal(err)
	}
	logger.maxBytes = 512
	ctx := WithRequest(WithLogger(t.Context(), logger), "secret-request-marker", "quota", "manual")
	Emit(ctx, Event{Stage: "rpc_read", Outcome: "failed", Reason: "timeout"})
	Emit(ctx, Event{Stage: "runner", Outcome: "failed", Reason: "secret-error-marker"})
	for range 30 {
		Emit(ctx, Event{Stage: "refresh", Outcome: "started"})
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, "logs", entry.Name())
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private file: %v %v", info, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(entry.Name(), logName) && info.Size() > logger.maxBytes {
			t.Fatalf("log exceeded cap: %s %d", entry.Name(), info.Size())
		}
		for _, marker := range []string{"secret-request-marker", "secret-error-marker", root} {
			if strings.Contains(string(content), marker) {
				t.Fatalf("diagnostic leaked %q", marker)
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(root, "logs", "refresh-faults.json"))
	if err != nil {
		t.Fatal(err)
	}
	var faults map[string]Fault
	if err := json.Unmarshal(content, &faults); err != nil {
		t.Fatal(err)
	}
	if faults["quota"].First.Reason != "timeout" || faults["quota"].Latest.Reason != "unknown" {
		t.Fatalf("first failure lost: %#v", faults)
	}
	logger, err = Open(root, "0.14.3")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	Emit(WithRequest(WithLogger(t.Context(), logger), "claim", "quota", "recovery"), Event{Stage: "recover_claim", Outcome: "recovered"})
	if len(logger.Faults()) == 0 {
		t.Fatal("claim release incorrectly resolved source failure")
	}
	Emit(WithRequest(WithLogger(t.Context(), logger), "next", "quota", "scheduled"), Event{Stage: "refresh", Outcome: "succeeded"})
	if len(logger.Faults()) != 0 {
		t.Fatal("recovery did not resolve retained fault")
	}
}

func TestRefreshDiagnosticsWriteFailureRecoversAndRetainsFirstFault(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	logger, err := Open(root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	ctx := WithRequest(WithLogger(t.Context(), logger), "request", "quota", "manual")
	_ = logger.file.Close()
	Emit(ctx, Event{Stage: "rpc_read", Outcome: "failed", Reason: "timeout"})
	if logger.Dropped() == 0 || logger.Faults()["quota"].First.Reason != "timeout" {
		t.Fatal("diagnostic loss silent or original fault lost")
	}
	logger.file = nil
	Emit(ctx, Event{Stage: "refresh", Outcome: "succeeded"})
	if len(logger.Faults()) != 0 {
		t.Fatal("log recovery did not resolve fault")
	}
}

func TestRefreshDiagnosticsRejectLinksAndDoNotInterruptBusiness(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "untouched")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "logs", "refresh.jsonl")); err != nil {
		t.Fatal(err)
	}
	logger, err := Open(root, "dev")
	if err == nil {
		logger.Close()
		t.Fatal("accepted symbolic link")
	}
	Emit(context.Background(), Event{Stage: "refresh", Outcome: "succeeded"})
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "untouched" {
		t.Fatal("modified symlink target")
	}
}

func TestRefreshDiagnosticsRetainFaultAcrossOrdinaryLogExpiry(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	logger, err := Open(root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	Emit(WithRequest(WithLogger(t.Context(), logger), "old", "runtime", "startup"), Event{Stage: "runner", Outcome: "failed", Reason: "worker_panic"})
	logger.Close()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "logs", "refresh.jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
	logger, err = Open(root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	if logger.Faults()["runtime"].First.Reason != "worker_panic" {
		t.Fatal("lost unresolved first fault")
	}
	content, err := os.ReadFile(filepath.Join(root, "logs", "refresh.jsonl"))
	if err != nil || len(content) != 0 {
		t.Fatal("expired log not pruned")
	}
}
