package preferences

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	dataStoreKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
	versionPattern      = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)
	attemptIDPattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func validatePreferences(snapshot Snapshot) error {
	if snapshot.SchemaVersion != CurrentPreferencesSchemaVersion || snapshot.Revision == 0 ||
		snapshot.Onboarding.Version != CurrentOnboardingVersion || !snapshot.Onboarding.Completed ||
		validateConfirmedSource(snapshot.CodexHome.Source) != nil || snapshot.CodexHome.Generation == 0 ||
		!validDataStoreKey(snapshot.CodexHome.DataStoreKey) || !validRefreshPreferences(snapshot.Refresh) ||
		!validUpdatePreferences(snapshot.Updates) || !validUIPreferences(snapshot.UI) {
		return ErrInvalidPreferences
	}
	if snapshot.PendingSwitch != nil && !validHomeSwitchJournal(*snapshot.PendingSwitch, snapshot.CodexHome) {
		return ErrInvalidPreferences
	}
	if snapshot.PendingResume != nil &&
		(snapshot.PendingSwitch != nil || !validHomeResumeJournal(*snapshot.PendingResume, snapshot.CodexHome)) {
		return ErrInvalidPreferences
	}
	if !validDetachedHomes(snapshot.DetachedHomes, snapshot.CodexHome, snapshot.PendingSwitch) {
		return ErrInvalidPreferences
	}
	if snapshot.LastSwitch != nil && !validHomeSwitchAudit(*snapshot.LastSwitch) {
		return ErrInvalidPreferences
	}
	return nil
}

func validDetachedHomes(
	values []CodexHomePreferences,
	active CodexHomePreferences,
	pending *HomeSwitchJournal,
) bool {
	if len(values) > 64 {
		return false
	}
	keys := make(map[string]struct{}, len(values))
	for _, value := range values {
		if validateCodexHome(value) != nil || value.Generation >= active.Generation {
			return false
		}
		if value.DataStoreKey == active.DataStoreKey {
			if pending == nil || pending.Target.DataStoreKey != value.DataStoreKey ||
				!sameSourceIdentity(pending.Target.Source, value.Source) {
				return false
			}
		}
		if _, exists := keys[value.DataStoreKey]; exists {
			return false
		}
		keys[value.DataStoreKey] = struct{}{}
	}
	return true
}

func validateConfirmedSource(source ConfirmedSource) error {
	if !filepath.IsAbs(source.Path) || filepath.Clean(source.Path) != source.Path ||
		len(source.Path) > 4096 || source.DeviceID == "" || len(source.DeviceID) > 256 ||
		source.Inode <= 0 || source.ConfirmedAtMS <= 0 {
		return ErrInvalidPreferences
	}
	return nil
}

func validDataStoreKey(value string) bool {
	return dataStoreKeyPattern.MatchString(value) && !strings.Contains(value, "..")
}

func validRefreshPreferences(value RefreshPreferences) bool {
	return value.QuotaIntervalSeconds >= 60 && value.QuotaIntervalSeconds <= 1800 &&
		value.ResetCreditsIntervalSeconds >= 60 && value.ResetCreditsIntervalSeconds <= 86400 &&
		value.ReconcileIntervalSeconds >= 60 && value.ReconcileIntervalSeconds <= 86400 &&
		value.JSONLDebounceMilliseconds >= 3000 && value.JSONLDebounceMilliseconds <= 5000
}

func validUpdatePreferences(value UpdatePreferences) bool {
	if value.AutoDownloadEnabled || value.Channel != UpdateChannelStable ||
		value.CheckIntervalSeconds < 3600 || value.CheckIntervalSeconds > 86400 ||
		!validOptionalVersion(value.SkippedVersion) || !validOptionalTimestamp(value.SnoozeUntilMS) ||
		!validOptionalTimestamp(value.LastCheckAtMS) {
		return false
	}
	return true
}

func validOptionalVersion(value *string) bool {
	return value == nil || versionPattern.MatchString(*value)
}

func validOptionalTimestamp(value *int64) bool {
	return value == nil || *value >= 0
}

func validUIPreferences(value UIPreferences) bool {
	if value.Locale != "zh-CN" {
		return false
	}
	if value.LaunchBehavior != LaunchBehaviorMainWindow && value.LaunchBehavior != LaunchBehaviorTray {
		return false
	}
	return value.OverviewRange == OverviewRangeToday || value.OverviewRange == OverviewRangeSevenDays ||
		value.OverviewRange == OverviewRangeThirtyDays
}

func validHomeSwitchStrategy(value HomeSwitchStrategy) bool {
	return value == HomeSwitchIndependentDatabase || value == HomeSwitchClearAndRebuild
}

func validHomeSwitchJournal(value HomeSwitchJournal, active CodexHomePreferences) bool {
	if value.SwitchID == "" || len(value.SwitchID) > 128 || !attemptIDPattern.MatchString(value.AttemptID) ||
		!validHomeSwitchStrategy(value.Strategy) ||
		value.StartedAtMS <= 0 || value.Previous.Generation == math.MaxUint64 ||
		value.Target.Generation != value.Previous.Generation+1 ||
		validateCodexHome(value.Previous) != nil || validateCodexHome(value.Target) != nil ||
		active != value.Target {
		return false
	}
	if sameSourceIdentity(value.Previous.Source, value.Target.Source) {
		return false
	}
	switch value.Strategy {
	case HomeSwitchIndependentDatabase:
		return value.Target.DataStoreKey != value.Previous.DataStoreKey
	case HomeSwitchClearAndRebuild:
		return value.Target.DataStoreKey == value.Previous.DataStoreKey
	default:
		return false
	}
}

func validateCodexHome(value CodexHomePreferences) error {
	if validateConfirmedSource(value.Source) != nil || value.Generation == 0 || !validDataStoreKey(value.DataStoreKey) {
		return ErrInvalidPreferences
	}
	return nil
}

func validHomeResumeJournal(value HomeResumeJournal, active CodexHomePreferences) bool {
	return value.SwitchID != "" && len(value.SwitchID) <= 128 &&
		attemptIDPattern.MatchString(value.AttemptID) &&
		validHomeSwitchStrategy(value.Strategy) && value.Generation == active.Generation &&
		value.Generation != math.MaxUint64 && value.TargetGeneration == value.Generation+1 &&
		value.StartedAtMS > 0
}

func validHomeSwitchAudit(value HomeSwitchAudit) bool {
	if value.SwitchID == "" || len(value.SwitchID) > 128 || !validHomeSwitchStrategy(value.Strategy) ||
		value.FromGeneration == 0 || value.ToGeneration != value.FromGeneration+1 || value.FinishedAtMS <= 0 {
		return false
	}
	return value.Outcome == HomeSwitchCompleted || value.Outcome == HomeSwitchRolledBack
}

func sameSourceIdentity(left, right ConfirmedSource) bool {
	return left.Path == right.Path && left.DeviceID == right.DeviceID && left.Inode == right.Inode
}

func nextRevision(current uint64) (uint64, error) {
	if current == 0 || current == math.MaxUint64 {
		return 0, ErrInvalidPreferences
	}
	return current + 1, nil
}
