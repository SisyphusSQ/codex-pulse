//go:build darwin && cgo && upgradee2e

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeE2EControlRejectsEscapingAndBroadPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	runtime := filepath.Join(root, "runtime")
	for _, directory := range []string{data, runtime} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	control := upgradeE2EControl{
		Version: controlVersionForTest(), Root: root, DataDirectory: data, RuntimeDir: runtime,
		Preferences: filepath.Join(data, "preferences.json"), Evidence: filepath.Join(root, "evidence.jsonl"),
		Result: filepath.Join(root, "result.json"), Scenario: "success", Action: "update",
		SourceVersion: "0.1.0", TargetVersion: "0.2.0", MarkerSession: "marker", ExpectedSchema: 14,
	}
	if err := validateUpgradeE2EControl(control, root); err != nil {
		t.Fatalf("valid control: %v", err)
	}
	escaping := control
	escaping.Result = filepath.Join(filepath.Dir(root), "escaped-result.json")
	if err := validateUpgradeE2EControl(escaping, root); err == nil {
		t.Fatal("control accepted an escaping output")
	}
	if err := os.Chmod(runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateUpgradeE2EControl(control, root); err == nil {
		t.Fatal("control accepted a broadly readable runtime directory")
	}
}

func controlVersionForTest() string { return "upgrade-e2e-v1" }

func TestUpgradeE2ERollbackMarkerReadbackNeverSeedsMissingFact(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	defer func() { _ = database.Close(t.Context()) }()

	const marker = "upgrade-e2e-existing-marker"
	present, err := prepareUpgradeE2ESourceMarker(t.Context(), repository, marker, false)
	if err == nil || present {
		t.Fatalf("missing rollback marker = present:%v err:%v", present, err)
	}
	if _, readErr := repository.Session(t.Context(), marker); readErr == nil {
		t.Fatal("rollback readback wrote a missing marker")
	}

	present, err = prepareUpgradeE2ESourceMarker(t.Context(), repository, marker, true)
	if err != nil || !present {
		t.Fatalf("initial source marker = present:%v err:%v", present, err)
	}
	present, err = prepareUpgradeE2ESourceMarker(t.Context(), repository, marker, false)
	if err != nil || !present {
		t.Fatalf("existing rollback marker = present:%v err:%v", present, err)
	}
	stored, err := repository.Session(t.Context(), marker)
	if err != nil || stored.SessionID != marker {
		t.Fatalf("stored marker = %#v err:%v", stored, err)
	}
}
