package app

import "github.com/wailsapp/wails/v3/pkg/application"

const (
	UpdateStateChangedEventName       = "codex-pulse:update-state-changed"
	UpdateStateChangedContractVersion = "update-state-changed-v1"
)

// UpdateStateChangedEvent is only an invalidation hint. Update metadata is
// loaded through the allowlisted UpdateState query instead of being broadcast.
type UpdateStateChangedEvent struct {
	Version string `json:"version"`
}

func init() {
	application.RegisterEvent[UpdateStateChangedEvent](UpdateStateChangedEventName)
}
