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
	value := Snapshot{
		SchemaVersion: CurrentPreferencesSchemaVersion,
		Revision:      1,
		Onboarding: OnboardingPreferences{
			Version: CurrentOnboardingVersion, Completed: true,
		},
		CodexHome: CodexHomePreferences{
			Source: onboarding.CodexHome, Generation: 1, DataStoreKey: DefaultDataStoreKey,
		},
		Online: OnlinePreferences{
			QuotaEnabled: onboarding.OnlineQuotaEnabled, ResetCreditsEnabled: onboarding.ResetCreditsEnabled,
		},
		Refresh: DefaultRefreshPreferences(), Updates: DefaultUpdatePreferences(), UI: DefaultUIPreferences(),
	}
	if err := validatePreferences(value); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func onboardingFromPreferences(value Snapshot) (OnboardingSnapshot, error) {
	if err := validatePreferences(value); err != nil {
		return OnboardingSnapshot{}, err
	}
	return OnboardingSnapshot{
		SchemaVersion: CurrentSchemaVersion, OnboardingVersion: CurrentOnboardingVersion,
		OnboardingCompleted: true, CodexHome: value.CodexHome.Source,
		OnlineQuotaEnabled:  value.Online.QuotaEnabled,
		ResetCreditsEnabled: value.Online.ResetCreditsEnabled,
	}, nil
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
	case CurrentPreferencesSchemaVersion:
		current, err := decodeCurrentPreferences(content)
		return current, false, err
	default:
		return Snapshot{}, false, fmt.Errorf("%w: unsupported schema version", ErrInvalidPreferences)
	}
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
