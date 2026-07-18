package app

import (
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const PlatformChangedEventName = "codex-pulse:platform-changed"

func init() {
	application.RegisterEvent[platformtray.PlatformChange](PlatformChangedEventName)
}
