package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSelectedScenarios(t *testing.T) {
	all, err := selectedScenarios("all")
	if err != nil || len(all) != 5 || all[0] != "success" || all[3] != "information_only" || all[4] != "migration_failure" {
		t.Fatalf("selectedScenarios(all) = %#v, %v", all, err)
	}
	if _, err := selectedScenarios("unknown"); err == nil {
		t.Fatal("selectedScenarios accepted unknown scenario")
	}
}

func TestUpgradeE2ERequiresExplicitOptIn(t *testing.T) {
	t.Setenv("CODEX_PULSE_RUN_UPGRADE_E2E", "")
	if err := requireUpgradeE2EOptIn(); err == nil {
		t.Fatal("upgrade E2E accepted a missing opt-in")
	}
	t.Setenv("CODEX_PULSE_RUN_UPGRADE_E2E", "1")
	if err := requireUpgradeE2EOptIn(); err != nil {
		t.Fatal(err)
	}
}

func TestUserStateGuardPreservesPreexistingAppcastCache(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "Library", "Caches", "Sparkle_generate_appcast")
	preferences := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(preferences, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "preexisting"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := &userStateGuard{home: home, appcastCacheEntries: map[string]struct{}{"preexisting": {}}}
	if err := os.Mkdir(filepath.Join(cache, "created-by-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preferences, "com.sisyphussq.codexpulse.upgradee2e.test.plist"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.cleanupAndVerify(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "preexisting")); err != nil {
		t.Fatalf("preexisting cache was not preserved: %v", err)
	}
}

func TestRedactOutputRemovesSyntheticSecret(t *testing.T) {
	if got := redactOutput([]byte("before secret after"), "secret"); got != "before [REDACTED] after" {
		t.Fatalf("redactOutput = %q", got)
	}
}

func TestValidateTerminalResult(t *testing.T) {
	valid := result{Stage: "target_verified", Application: targetVersion, SchemaVersion: targetSchema, MarkerPresent: true, BackupPresent: true}
	if err := validateTerminalResult("success", valid); err != nil {
		t.Fatal(err)
	}
	valid.MarkerPresent = false
	if err := validateTerminalResult("success", valid); err == nil {
		t.Fatal("success result without marker was accepted")
	}
}

func TestUpgradeFixtureTracksCurrentSchemaAndImmediatePredecessor(t *testing.T) {
	if targetSchema != 15 {
		t.Fatalf("targetSchema = %d, want current schema 15", targetSchema)
	}
	if sourceSchema != targetSchema-1 {
		t.Fatalf("sourceSchema = %d, want targetSchema-1", sourceSchema)
	}
}

func TestWaitProcessRejectsNonZeroHelperExit(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 7")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := trackFixtureHelper(command)
	if err := waitProcess(process, "", time.Second); err == nil {
		t.Fatal("non-zero helper exit was accepted")
	}
}

func TestFixtureProcessCleanupTerminatesAndVerifiesIdentity(t *testing.T) {
	command := exec.Command("/bin/sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := trackFixtureHelper(command)
	process.appPID = command.Process.Pid
	output, err := exec.Command("ps", "-p", strconv.Itoa(process.appPID), "-o", "command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	process.executable = strings.TrimSpace(string(output))
	if err := process.cleanup(""); err != nil {
		t.Fatal(err)
	}
	if processMatches(process.appPID, process.executable) {
		t.Fatal("identity-matched fixture remained alive")
	}
}

func TestFixtureBundleIdentifierIsStableAndIsolated(t *testing.T) {
	first := fixtureBundleIdentifier("/private/tmp/run-a/success")
	if first != fixtureBundleIdentifier("/private/tmp/run-a/success") {
		t.Fatal("bundle identifier is not stable within one scenario")
	}
	if first == fixtureBundleIdentifier("/private/tmp/run-b/success") {
		t.Fatal("bundle identifier is shared across isolated runs")
	}
	if !strings.HasPrefix(first, "com.sisyphussq.codexpulse.upgradee2e.") {
		t.Fatalf("unexpected bundle identifier %q", first)
	}
}
