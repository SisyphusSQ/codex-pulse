package runtimeinfo

import (
	"context"
	"errors"
	"regexp"
	"strconv"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

var safeVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)

func (service *Service) Settings(ctx context.Context) (SettingsResponse, error) {
	if service == nil || service.preferences == nil {
		return SettingsResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	snapshot, err := service.preferences.LoadPreferences(ctx)
	if err != nil {
		return SettingsResponse{}, runtimeReadFailure(err)
	}
	mapped, err := mapSettings(snapshot)
	if err != nil {
		return SettingsResponse{}, basequery.NewUnavailableFailure(err)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{
		Meta: meta, Snapshot: mapped, EditableFields: settingsEditableFields(),
	}, nil
}

func mapSettings(snapshot preferences.Snapshot) (SettingsSnapshot, error) {
	if snapshot.SchemaVersion != preferences.CurrentPreferencesSchemaVersion || snapshot.Revision == 0 ||
		snapshot.Onboarding.Version != preferences.CurrentOnboardingVersion || !snapshot.Onboarding.Completed ||
		snapshot.CodexHome.Generation == 0 || !validSettingsRefresh(snapshot.Refresh) ||
		!validSettingsUpdates(snapshot.Updates) || !validSettingsUI(snapshot.UI) {
		return SettingsSnapshot{}, errors.New("preferences snapshot is invalid")
	}
	snooze, err := optionalNumeric(
		snapshot.Updates.SnoozeUntilMS, basequery.NumericMilliseconds, basequery.UnknownNotApplicable,
	)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	lastCheck, err := optionalNumeric(
		snapshot.Updates.LastCheckAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
	)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	switchStatus := HomeSwitchStable
	if snapshot.PendingResume != nil {
		switchStatus = HomeSwitchRecoveryRequired
	} else if snapshot.PendingSwitch != nil {
		switchStatus = HomeSwitchPending
	}
	var lastOutcome *string
	if snapshot.LastSwitch != nil {
		value := string(snapshot.LastSwitch.Outcome)
		if value != string(preferences.HomeSwitchCompleted) &&
			value != string(preferences.HomeSwitchRolledBack) {
			return SettingsSnapshot{}, errors.New("preferences switch outcome is invalid")
		}
		lastOutcome = &value
	}
	return SettingsSnapshot{
		SchemaVersion: snapshot.SchemaVersion, Revision: strconv.FormatUint(snapshot.Revision, 10),
		OnboardingCompleted: snapshot.Onboarding.Completed,
		Home: SettingsHomeSnapshot{
			Configured: true, Generation: strconv.FormatUint(snapshot.CodexHome.Generation, 10),
			SwitchStatus: switchStatus, LastSwitchOutcome: lastOutcome,
		},
		Online: SettingsOnlineSnapshot{
			QuotaEnabled:        snapshot.Online.QuotaEnabled,
			ResetCreditsEnabled: snapshot.Online.ResetCreditsEnabled,
		},
		Refresh: SettingsRefreshSnapshot{
			QuotaIntervalSeconds:        snapshot.Refresh.QuotaIntervalSeconds,
			ResetCreditsIntervalSeconds: snapshot.Refresh.ResetCreditsIntervalSeconds,
			ReconcileIntervalSeconds:    snapshot.Refresh.ReconcileIntervalSeconds,
			JSONLDebounceMilliseconds:   snapshot.Refresh.JSONLDebounceMilliseconds,
		},
		Updates: SettingsUpdateSnapshot{
			AutoCheckEnabled:     snapshot.Updates.AutoCheckEnabled,
			AutoDownloadEnabled:  snapshot.Updates.AutoDownloadEnabled,
			Channel:              string(snapshot.Updates.Channel),
			CheckIntervalSeconds: snapshot.Updates.CheckIntervalSeconds,
			SkippedVersion:       cloneStringPointer(snapshot.Updates.SkippedVersion),
			SnoozeUntilMS:        snooze, LastCheckAtMS: lastCheck,
		},
		UI: SettingsUISnapshot{
			Locale: snapshot.UI.Locale, LaunchBehavior: string(snapshot.UI.LaunchBehavior),
			OverviewRange: string(snapshot.UI.OverviewRange),
		},
	}, nil
}

func validSettingsRefresh(value preferences.RefreshPreferences) bool {
	return value.QuotaIntervalSeconds >= 60 && value.QuotaIntervalSeconds <= 1800 &&
		value.ResetCreditsIntervalSeconds >= 60 && value.ResetCreditsIntervalSeconds <= 86400 &&
		value.ReconcileIntervalSeconds >= 60 && value.ReconcileIntervalSeconds <= 86400 &&
		value.JSONLDebounceMilliseconds >= 3000 && value.JSONLDebounceMilliseconds <= 5000
}

func validSettingsUpdates(value preferences.UpdatePreferences) bool {
	return !value.AutoDownloadEnabled && value.Channel == preferences.UpdateChannelStable &&
		value.CheckIntervalSeconds >= 3600 && value.CheckIntervalSeconds <= 86400 &&
		(value.SkippedVersion == nil || safeVersionPattern.MatchString(*value.SkippedVersion)) &&
		(value.SnoozeUntilMS == nil || *value.SnoozeUntilMS >= 0) &&
		(value.LastCheckAtMS == nil || *value.LastCheckAtMS >= 0)
}

func validSettingsUI(value preferences.UIPreferences) bool {
	return value.Locale == "zh-CN" &&
		(value.LaunchBehavior == preferences.LaunchBehaviorMainWindow ||
			value.LaunchBehavior == preferences.LaunchBehaviorTray) &&
		(value.OverviewRange == preferences.OverviewRangeQuotaWeek ||
			value.OverviewRange == preferences.OverviewRangeToday ||
			value.OverviewRange == preferences.OverviewRangeSevenDays ||
			value.OverviewRange == preferences.OverviewRangeThirtyDays)
}

func settingsEditableFields() []EditableField {
	return []EditableField{
		booleanField("online.quotaEnabled", true),
		booleanField("online.resetCreditsEnabled", true),
		integerField("refresh.quotaIntervalSeconds", true, 60, 1800),
		integerField("refresh.resetCreditsIntervalSeconds", true, 60, 86400),
		integerField("refresh.reconcileIntervalSeconds", true, 60, 86400),
		integerField("refresh.jsonlDebounceMilliseconds", true, 3000, 5000),
		booleanField("updates.autoCheckEnabled", true),
		booleanField("updates.autoDownloadEnabled", false),
		enumField("updates.channel", false, []string{"stable"}),
		integerField("updates.checkIntervalSeconds", true, 3600, 86400),
		enumField("ui.locale", false, []string{"zh-CN"}),
		enumField("ui.launchBehavior", true, []string{"main_window", "tray"}),
		enumField("ui.overviewRange", true, []string{"quota_week", "today", "seven_days", "thirty_days"}),
	}
}

func booleanField(key string, editable bool) EditableField {
	return EditableField{
		Key: key, Type: EditableBoolean, Editable: editable, Options: make([]string, 0),
	}
}

func integerField(key string, editable bool, minimum int64, maximum int64) EditableField {
	return EditableField{
		Key: key, Type: EditableInteger, Editable: editable,
		Minimum: &minimum, Maximum: &maximum, Options: make([]string, 0),
	}
}

func enumField(key string, editable bool, options []string) EditableField {
	return EditableField{
		Key: key, Type: EditableEnum, Editable: editable,
		Options: append([]string(nil), options...),
	}
}
