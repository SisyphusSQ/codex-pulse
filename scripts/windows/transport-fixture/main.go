// transport-fixture seeds only synthetic development state. It is not shipped with the App.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func main() {
	if len(os.Args) != 3 || !filepath.IsAbs(os.Args[1]) || (os.Args[2] != "normal" && os.Args[2] != "recovery" && os.Args[2] != "recovery-restore") {
		panic("invalid fixture arguments")
	}
	ctx := context.Background()
	runtime := os.Args[1]
	must(platformfs.MkdirAll(runtime, 0o700))
	must(platformfs.MkdirAll(filepath.Join(runtime, "synthetic-home"), 0o700))
	snapshot, err := preferences.NewCurrentSnapshot(nil, preferences.OnlinePreferences{})
	must(err)
	disabled := preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled}
	snapshot.Providers = preferences.ProviderPreferences{Codex: disabled, Cursor: disabled, Grok: disabled, DSH: disabled}
	snapshot.Updates.AutoCheckEnabled = false
	store, err := preferences.NewFileStore(filepath.Join(runtime, "preferences.json"))
	must(err)
	must(store.InitializePreferences(ctx, snapshot))
	if os.Args[2] == "recovery" || os.Args[2] == "recovery-restore" {
		database, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(runtime, "codex-pulse.db")})
		must(err)
		if os.Args[2] == "recovery-restore" {
			must(factstore.NewRepository(database).EnsureApplicationSchema(ctx))
			_, err = database.Backup(ctx, storesqlite.BackupOptions{Destination: filepath.Join(runtime, "backups", "known-good.db")})
			must(err)
		}
		must(database.Write(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.WithContext(ctx).Exec("PRAGMA user_version = 9999").Error
		}))
		must(database.Close(ctx))
	}
	fmt.Println("synthetic transport fixture initialized")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
