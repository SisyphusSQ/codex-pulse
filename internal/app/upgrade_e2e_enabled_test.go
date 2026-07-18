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
