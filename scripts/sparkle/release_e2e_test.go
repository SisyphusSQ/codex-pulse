package sparkle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyntheticReleaseEndToEnd(t *testing.T) {
	if os.Getenv("CODEX_PULSE_RUN_RELEASE_E2E") != "1" {
		t.Skip("set CODEX_PULSE_RUN_RELEASE_E2E=1 for the local synthetic-key release gate")
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed := base64.StdEncoding.EncodeToString(private.Seed())
	publicKey := base64.StdEncoding.EncodeToString(public)
	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("Synthetic local update verification only.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "scripts/sparkle/release.sh", "9.8.7", "987",
		"https://updates.example.test/appcast.xml", "https://downloads.example.test/releases", publicKey, notes)
	command.Dir = root
	command.Stdin = strings.NewReader(seed + "\n")
	command.Env = append(os.Environ(), "PATH=/tmp/codex-pulse-tools/bin:"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("synthetic release: %v\n%s", err, output)
	}
	if strings.Contains(string(output), seed) {
		t.Fatal("release output leaked the private seed")
	}
	for _, name := range []string{"appcast.xml", "manifest.json", "Codex-Pulse-9.8.7-arm64.zip", "Codex-Pulse-9.8.7-arm64.txt"} {
		data, err := os.ReadFile(filepath.Join(root, "dist", "update", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), seed) {
			t.Fatalf("%s leaked the private seed", name)
		}
	}
	manifestPath := filepath.Join(root, "dist", "update", "manifest.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(root, "dist", ".update.lock")
	ready := filepath.Join(t.TempDir(), "lock-ready")
	holder := exec.Command("bash", "-c", `exec 9>"$1"; /usr/bin/lockf -s -t 0 9 || exit 1; touch "$2"; sleep 60`, "lock-holder", lockFile, ready)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if holder.Process != nil {
			_ = holder.Process.Kill()
		}
		_ = holder.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock holder did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	locked := exec.Command("bash", "scripts/sparkle/release.sh", "9.8.7", "987",
		"https://updates.example.test/appcast.xml", "https://downloads.example.test/releases", publicKey, notes)
	locked.Dir = root
	locked.Stdin = strings.NewReader(seed + "\n")
	locked.Env = command.Env
	lockedOutput, lockedErr := locked.CombinedOutput()
	if lockedErr == nil || strings.Contains(string(lockedOutput), seed) {
		t.Fatalf("live release lock was not enforced safely: err=%v output=%s", lockedErr, lockedOutput)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	holder.Process = nil
	postCrash := exec.Command("bash", "scripts/sparkle/release.sh", "invalid-version", "987",
		"https://updates.example.test/appcast.xml", "https://downloads.example.test/releases", publicKey, notes)
	postCrash.Dir = root
	postCrash.Stdin = strings.NewReader(seed + "\n")
	postCrash.Env = command.Env
	if postCrashOutput, postCrashErr := postCrash.CombinedOutput(); postCrashErr == nil || strings.Contains(string(postCrashOutput), seed) {
		t.Fatalf("post-crash lock recovery did not fail safely: err=%v output=%s", postCrashErr, postCrashOutput)
	}
	if info, err := os.Lstat(lockFile); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("release lock is not a regular file: info=%v err=%v", info, err)
	}
	_, mismatchedPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedSeed := base64.StdEncoding.EncodeToString(mismatchedPrivate.Seed())
	failure := exec.Command("bash", "scripts/sparkle/release.sh", "9.8.7", "987",
		"https://updates.example.test/appcast.xml", "https://downloads.example.test/releases", publicKey, notes)
	failure.Dir = root
	failure.Stdin = strings.NewReader(mismatchedSeed + "\n")
	failure.Env = command.Env
	failureOutput, failureErr := failure.CombinedOutput()
	if failureErr == nil {
		t.Fatal("invalid private key unexpectedly published a release")
	}
	if strings.Contains(string(failureOutput), mismatchedSeed) {
		t.Fatal("failed release leaked the private-key canary")
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed release replaced the previously verified manifest")
	}
	staging, err := filepath.Glob(filepath.Join(root, "dist", ".update.staging.*"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("failed release left staging directories: %v, err=%v", staging, err)
	}
}
