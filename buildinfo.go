package main

import (
	"encoding/json"
	"io"
	"runtime"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// writeBuildInfo emits software compatibility metadata without opening a Home,
// runtime, database, credentials or network connection.
func writeBuildInfo(output io.Writer) error {
	return json.NewEncoder(output).Encode(struct {
		Version           string
		OperatingSystem   string
		Architecture      string
		CoreContract      string
		DatabaseSchema    int
		PreferencesSchema int
		CredentialSchema  string
	}{applicationVersion, runtime.GOOS, runtime.GOARCH, core.ContractVersion,
		store.ApplicationSchemaVersion(), preferences.CurrentPreferencesSchemaVersion,
		apisubscriptions.CredentialSchemaFingerprint()})
}
