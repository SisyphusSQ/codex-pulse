package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestBuildInfoUsesCompiledCompatibilityWithoutOpeningRuntime(t *testing.T) {
	t.Setenv("CODEX_HOME", "invalid-unopened-home")
	var output bytes.Buffer
	if err := writeBuildInfo(&output); err != nil {
		t.Fatal(err)
	}
	var value struct {
		Version, CoreContract, CredentialSchema string
		DatabaseSchema                          int
	}
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Version != applicationVersion || value.CoreContract != core.ContractVersion || value.DatabaseSchema != store.ApplicationSchemaVersion() || len(value.CredentialSchema) != 64 {
		t.Fatal("software metadata does not match compiled contracts")
	}
}
