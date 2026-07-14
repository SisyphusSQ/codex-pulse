package preferences

import (
	"errors"
	"path/filepath"
)

const (
	CurrentSchemaVersion     = 1
	CurrentOnboardingVersion = 1
)

var (
	ErrNotConfigured       = errors.New("preferences not configured")
	ErrUnsafePath          = errors.New("unsafe preferences path")
	ErrInvalidPreferences  = errors.New("invalid preferences")
	ErrAlreadyConfirmed    = errors.New("onboarding already confirmed")
	ErrDurabilityUnknown   = errors.New("preferences published with unknown durability")
	ErrPreferencesConflict = errors.New("preferences revision conflict")
)

type ConfirmedSource struct {
	Path          string `json:"path"`
	DeviceID      string `json:"device_id"`
	Inode         int64  `json:"inode"`
	ConfirmedAtMS int64  `json:"confirmed_at_ms"`
}

type OnboardingSnapshot struct {
	SchemaVersion       int             `json:"schema_version"`
	OnboardingVersion   int             `json:"onboarding_version"`
	OnboardingCompleted bool            `json:"onboarding_completed"`
	CodexHome           ConfirmedSource `json:"codex_home"`
	OnlineQuotaEnabled  bool            `json:"online_quota_enabled"`
	ResetCreditsEnabled bool            `json:"reset_credits_enabled"`
}

func validateSnapshot(snapshot OnboardingSnapshot) error {
	if snapshot.SchemaVersion != CurrentSchemaVersion ||
		snapshot.OnboardingVersion != CurrentOnboardingVersion ||
		!snapshot.OnboardingCompleted ||
		!filepath.IsAbs(snapshot.CodexHome.Path) ||
		filepath.Clean(snapshot.CodexHome.Path) != snapshot.CodexHome.Path ||
		snapshot.CodexHome.DeviceID == "" ||
		snapshot.CodexHome.Inode <= 0 ||
		snapshot.CodexHome.ConfirmedAtMS <= 0 {
		return ErrInvalidPreferences
	}
	return nil
}

func sameConfirmation(left, right OnboardingSnapshot) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.OnboardingVersion == right.OnboardingVersion &&
		left.OnboardingCompleted == right.OnboardingCompleted &&
		left.CodexHome.Path == right.CodexHome.Path &&
		left.CodexHome.DeviceID == right.CodexHome.DeviceID &&
		left.CodexHome.Inode == right.CodexHome.Inode &&
		left.OnlineQuotaEnabled == right.OnlineQuotaEnabled &&
		left.ResetCreditsEnabled == right.ResetCreditsEnabled
}
