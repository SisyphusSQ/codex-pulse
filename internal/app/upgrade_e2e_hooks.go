package app

import (
	"github.com/SisyphusSQ/codex-pulse/internal/singleinstance"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type upgradeE2EOverrides struct {
	Store           storesqlite.Config
	Instance        singleinstance.Config
	PreferencesPath string
}
