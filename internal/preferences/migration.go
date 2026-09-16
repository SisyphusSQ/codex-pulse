package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func preferencesFromOnboarding(onboarding OnboardingSnapshot) (Snapshot, error) {
	if err := validateSnapshot(onboarding); err != nil {
		return Snapshot{}, err
	}
	value := newInitializedSnapshot(
		CodexHomePointer(CodexHomePreferences{
			Source: onboarding.CodexHome, Generation: 1, DataStoreKey: DefaultDataStoreKey,
		}),
		OnlinePreferences{
			QuotaEnabled: onboarding.OnlineQuotaEnabled, ResetCreditsEnabled: onboarding.ResetCreditsEnabled,
			CursorOnlineEnabled: true, GrokQuotaEnabled: true, GrokAutoRefreshEnabled: true,
		},
	)
	if err := validatePreferences(value); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func newInitializedSnapshot(home *CodexHomePreferences, online OnlinePreferences) Snapshot {
	return Snapshot{
		SchemaVersion: CurrentPreferencesSchemaVersion,
		Revision:      1,
		Onboarding: OnboardingPreferences{
			Version: CurrentOnboardingVersion, Completed: true,
		},
		CodexHome: CloneCodexHome(home),
		Providers: DefaultProviderPreferences(),
		Online:    online,
		Refresh:   DefaultRefreshPreferences(),
		Updates:   DefaultUpdatePreferences(),
		UI:        DefaultUIPreferences(),
	}
}

func NewV3Snapshot(home *CodexHomePreferences, online OnlinePreferences) (Snapshot, error) {
	value := newInitializedSnapshot(home, online)
	if err := validatePreferences(value); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func onboardingFromPreferences(value Snapshot) (OnboardingSnapshot, error) {
	if err := validatePreferences(value); err != nil {
		return OnboardingSnapshot{}, err
	}
	if value.CodexHome == nil {
		return OnboardingSnapshot{}, ErrNotConfigured
	}
	return OnboardingSnapshot{
		SchemaVersion: CurrentSchemaVersion, OnboardingVersion: CurrentOnboardingVersion,
		OnboardingCompleted: true, CodexHome: value.CodexHome.Source,
		OnlineQuotaEnabled:  value.Online.QuotaEnabled,
		ResetCreditsEnabled: value.Online.ResetCreditsEnabled,
	}, nil
}

type v2Snapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	Revision      uint64                 `json:"revision"`
	Onboarding    OnboardingPreferences  `json:"onboarding"`
	CodexHome     CodexHomePreferences   `json:"codex_home"`
	Online        v2OnlinePreferences    `json:"online"`
	Refresh       RefreshPreferences     `json:"refresh"`
	Updates       UpdatePreferences      `json:"updates"`
	UI            UIPreferences          `json:"ui"`
	DetachedHomes []CodexHomePreferences `json:"detached_homes,omitempty"`
	PendingSwitch *HomeSwitchJournal     `json:"pending_switch,omitempty"`
	PendingResume *HomeResumeJournal     `json:"pending_resume,omitempty"`
	LastSwitch    *HomeSwitchAudit       `json:"last_switch,omitempty"`
}

type v2OnlinePreferences struct {
	QuotaEnabled           bool  `json:"quota_enabled"`
	ResetCreditsEnabled    bool  `json:"reset_credits_enabled"`
	GrokQuotaEnabled       *bool `json:"grok_quota_enabled,omitempty"`
	GrokAutoRefreshEnabled *bool `json:"grok_auto_refresh_enabled,omitempty"`
}

func decodePreferences(content []byte) (Snapshot, bool, error) {
	if err := validateJSONDocument(content); err != nil {
		return Snapshot{}, false, err
	}
	var discriminator struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(content, &discriminator); err != nil || discriminator.SchemaVersion == nil {
		return Snapshot{}, false, fmt.Errorf("%w: malformed JSON", ErrInvalidPreferences)
	}
	switch *discriminator.SchemaVersion {
	case CurrentSchemaVersion:
		legacy, err := decodeSnapshot(content)
		if err != nil {
			return Snapshot{}, false, err
		}
		migrated, err := preferencesFromOnboarding(legacy)
		return migrated, true, err
	case preferencesSchemaV2:
		legacy, err := decodeV2Preferences(content)
		if err != nil {
			return Snapshot{}, false, err
		}
		migrated, err := migrateV2ToV3(legacy)
		return migrated, true, err
	case CurrentPreferencesSchemaVersion:
		current, err := decodeCurrentPreferences(content)
		return current, false, err
	default:
		return Snapshot{}, false, fmt.Errorf("%w: unsupported schema version", ErrInvalidPreferences)
	}
}

func decodeV2Preferences(content []byte) (v2Snapshot, error) {
	if err := validateV2JSONShape(content); err != nil {
		return v2Snapshot{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var value v2Snapshot
	if err := decoder.Decode(&value); err != nil {
		return v2Snapshot{}, fmt.Errorf("%w: malformed JSON", ErrInvalidPreferences)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return v2Snapshot{}, fmt.Errorf("%w: trailing JSON", ErrInvalidPreferences)
	}
	if value.SchemaVersion != preferencesSchemaV2 || value.Revision == 0 ||
		value.Onboarding.Version != CurrentOnboardingVersion || !value.Onboarding.Completed ||
		validateCodexHome(value.CodexHome) != nil || !validRefreshPreferences(value.Refresh) ||
		!validUpdatePreferences(value.Updates) || !validUIPreferences(value.UI) {
		return v2Snapshot{}, ErrInvalidPreferences
	}
	if value.PendingSwitch != nil && !validHomeSwitchJournal(*value.PendingSwitch, value.CodexHome) {
		return v2Snapshot{}, ErrInvalidPreferences
	}
	if value.PendingResume != nil &&
		(value.PendingSwitch != nil || !validHomeResumeJournal(*value.PendingResume, value.CodexHome)) {
		return v2Snapshot{}, ErrInvalidPreferences
	}
	active := value.CodexHome
	if !validDetachedHomes(value.DetachedHomes, &active, value.PendingSwitch) {
		return v2Snapshot{}, ErrInvalidPreferences
	}
	if value.LastSwitch != nil && !validHomeSwitchAudit(*value.LastSwitch) {
		return v2Snapshot{}, ErrInvalidPreferences
	}
	return value, nil
}

func migrateV2ToV3(value v2Snapshot) (Snapshot, error) {
	online := OnlinePreferences{
		QuotaEnabled:           value.Online.QuotaEnabled,
		ResetCreditsEnabled:    value.Online.ResetCreditsEnabled,
		CursorOnlineEnabled:    true,
		GrokQuotaEnabled:       true,
		GrokAutoRefreshEnabled: true,
	}
	if value.Online.GrokQuotaEnabled != nil {
		online.GrokQuotaEnabled = *value.Online.GrokQuotaEnabled
	}
	if value.Online.GrokAutoRefreshEnabled != nil {
		online.GrokAutoRefreshEnabled = *value.Online.GrokAutoRefreshEnabled
	}
	migrated := Snapshot{
		SchemaVersion: CurrentPreferencesSchemaVersion,
		Revision:      value.Revision,
		Onboarding:    value.Onboarding,
		CodexHome:     CodexHomePointer(value.CodexHome),
		Providers:     DefaultProviderPreferences(),
		Online:        online,
		Refresh:       value.Refresh,
		Updates:       cloneUpdatePreferences(value.Updates),
		UI:            value.UI,
		DetachedHomes: append([]CodexHomePreferences(nil), value.DetachedHomes...),
		PendingSwitch: cloneHomeSwitchJournal(value.PendingSwitch),
		PendingResume: cloneHomeResumeJournal(value.PendingResume),
		LastSwitch:    cloneHomeSwitchAudit(value.LastSwitch),
	}
	if err := validatePreferences(migrated); err != nil {
		return Snapshot{}, err
	}
	return migrated, nil
}

func decodeCurrentPreferences(content []byte) (Snapshot, error) {
	if err := validateCurrentJSONShape(content); err != nil {
		return Snapshot{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var value Snapshot
	if err := decoder.Decode(&value); err != nil {
		return Snapshot{}, fmt.Errorf("%w: malformed JSON", ErrInvalidPreferences)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Snapshot{}, fmt.Errorf("%w: trailing JSON", ErrInvalidPreferences)
	}
	if err := validatePreferences(value); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func marshalPreferences(value Snapshot) ([]byte, error) {
	if err := validatePreferences(value); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: encode preferences", ErrInvalidPreferences)
	}
	content = append(content, '\n')
	if len(content) > maximumPreferencesBytes {
		return nil, fmt.Errorf("%w: preferences exceed size limit", ErrInvalidPreferences)
	}
	return content, nil
}
