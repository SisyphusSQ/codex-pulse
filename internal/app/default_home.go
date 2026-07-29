package app

import (
	"context"
	"errors"
	"math"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/onboarding"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

type defaultCodexHomeConfiguration struct {
	HomePath            string
	TrackerDatabasePath string
	Store               onboarding.Store
	Probe               onboarding.HomeProbe
}

type defaultCodexHomeResult struct {
	Configured       bool
	IdentityMigrated bool
}

type defaultHomePreferencesStore interface {
	onboarding.Store
	LoadPreferences(context.Context) (preferences.Snapshot, error)
	CompareAndSwap(context.Context, uint64, preferences.Snapshot) error
}

func ensureDefaultCodexHomeConfigured(
	ctx context.Context,
	config defaultCodexHomeConfiguration,
) (defaultCodexHomeResult, error) {
	if config.HomePath == "" {
		return defaultCodexHomeResult{}, nil
	}
	if ctx == nil || config.Store == nil {
		return defaultCodexHomeResult{}, onboarding.ErrInvalidConfiguration
	}
	if _, err := config.Store.Load(ctx); err == nil {
		return migrateLegacyDefaultHomeIdentity(ctx, config)
	} else if !errors.Is(err, preferences.ErrNotConfigured) {
		return defaultCodexHomeResult{}, err
	}
	if config.Probe == nil {
		config.Probe = logsource.NewHomeProbe()
	}
	service, err := onboarding.NewService(onboarding.Config{
		Probe: config.Probe,
		Store: config.Store,
		Getenv: func(string) string {
			return ""
		},
		UserHomeDir: func() (string, error) {
			return "", nil
		},
		DefaultHome: func(string) string {
			return config.HomePath
		},
		TrackerDatabasePath: config.TrackerDatabasePath,
	})
	if err != nil {
		return defaultCodexHomeResult{}, err
	}
	state, err := service.Detect(ctx, "")
	if err != nil {
		return defaultCodexHomeResult{}, err
	}
	for _, candidate := range state.Candidates {
		if candidate.Source != onboarding.CandidateSourceDefault ||
			candidate.Status != onboarding.CandidateStatusReady {
			continue
		}
		if _, err := service.Confirm(ctx, onboarding.Confirmation{
			CandidateID:         candidate.ID,
			OnlineQuotaEnabled:  true,
			ResetCreditsEnabled: true,
		}); err != nil {
			return defaultCodexHomeResult{}, err
		}
		return defaultCodexHomeResult{Configured: true}, nil
	}
	return defaultCodexHomeResult{}, nil
}

func migrateLegacyDefaultHomeIdentity(
	ctx context.Context,
	config defaultCodexHomeConfiguration,
) (defaultCodexHomeResult, error) {
	store, ok := config.Store.(defaultHomePreferencesStore)
	if !ok {
		return defaultCodexHomeResult{}, nil
	}
	if config.Probe == nil {
		config.Probe = logsource.NewHomeProbe()
	}
	for attempt := 0; attempt < 3; attempt++ {
		current, err := store.LoadPreferences(ctx)
		if err != nil {
			return defaultCodexHomeResult{}, err
		}
		source := current.CodexHome.Source
		if homeidentity.IsStableDeviceID(source.DeviceID) {
			return defaultCodexHomeResult{}, nil
		}
		if !homeidentity.IsLegacyDeviceID(source.DeviceID) ||
			current.PendingSwitch != nil || current.PendingResume != nil ||
			current.Revision == math.MaxUint64 {
			return defaultCodexHomeResult{}, nil
		}
		metadata, err := config.Probe.Probe(ctx, config.HomePath)
		if err != nil {
			return defaultCodexHomeResult{}, nil
		}
		if metadata.Path != source.Path || metadata.Inode != source.Inode ||
			!homeidentity.IsStableDeviceID(metadata.DeviceID) {
			return defaultCodexHomeResult{}, nil
		}
		next := current
		next.Revision++
		next.CodexHome.Source.DeviceID = metadata.DeviceID
		if err := store.CompareAndSwap(ctx, current.Revision, next); err == nil {
			return defaultCodexHomeResult{IdentityMigrated: true}, nil
		} else if !errors.Is(err, preferences.ErrPreferencesConflict) {
			return defaultCodexHomeResult{}, err
		}
	}
	return defaultCodexHomeResult{}, preferences.ErrPreferencesConflict
}
