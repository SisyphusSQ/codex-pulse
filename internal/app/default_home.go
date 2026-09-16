package app

import (
	"context"
	"errors"
	"math"
	"time"

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
	InitializePreferences(context.Context, preferences.Snapshot) error
}

func ensureDefaultCodexHomeConfigured(
	ctx context.Context,
	config defaultCodexHomeConfiguration,
) (defaultCodexHomeResult, error) {
	if ctx == nil || config.Store == nil {
		return defaultCodexHomeResult{}, onboarding.ErrInvalidConfiguration
	}
	store, ok := config.Store.(defaultHomePreferencesStore)
	if !ok {
		return defaultCodexHomeResult{}, onboarding.ErrInvalidConfiguration
	}
	_, err := store.LoadPreferences(ctx)
	if err == nil {
		return migrateLegacyDefaultHomeIdentity(ctx, config)
	}
	if !errors.Is(err, preferences.ErrNotConfigured) {
		return defaultCodexHomeResult{}, err
	}
	home, configured, err := probeDefaultCodexHome(ctx, config)
	if err != nil {
		return defaultCodexHomeResult{}, err
	}
	snapshot, err := preferences.NewV3Snapshot(home, preferences.DefaultOnlinePreferences())
	if err != nil {
		return defaultCodexHomeResult{}, err
	}
	if err := store.InitializePreferences(ctx, snapshot); err != nil {
		return defaultCodexHomeResult{}, err
	}
	return defaultCodexHomeResult{Configured: configured}, nil
}

func probeDefaultCodexHome(
	ctx context.Context,
	config defaultCodexHomeConfiguration,
) (*preferences.CodexHomePreferences, bool, error) {
	if config.HomePath == "" {
		return nil, false, nil
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
		return nil, false, err
	}
	state, err := service.Detect(ctx, "")
	if err != nil {
		return nil, false, err
	}
	for _, candidate := range state.Candidates {
		if candidate.Source != onboarding.CandidateSourceDefault ||
			candidate.Status != onboarding.CandidateStatusReady {
			continue
		}
		current, probeErr := config.Probe.Probe(ctx, candidate.Path)
		if probeErr != nil || current.Path != candidate.Metadata.Path ||
			current.DeviceID != candidate.Metadata.DeviceID || current.Inode != candidate.Metadata.Inode {
			continue
		}
		return preferences.CodexHomePointer(preferences.CodexHomePreferences{
			Source: preferences.ConfirmedSource{
				Path: current.Path, DeviceID: current.DeviceID, Inode: current.Inode,
				ConfirmedAtMS: time.Now().UnixMilli(),
			},
			Generation: 1, DataStoreKey: preferences.DefaultDataStoreKey,
		}), true, nil
	}
	return nil, false, nil
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
		if current.CodexHome == nil {
			return defaultCodexHomeResult{}, nil
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
		home := *current.CodexHome
		home.Source.DeviceID = metadata.DeviceID
		next.CodexHome = &home
		if err := store.CompareAndSwap(ctx, current.Revision, next); err == nil {
			return defaultCodexHomeResult{IdentityMigrated: true}, nil
		} else if !errors.Is(err, preferences.ErrPreferencesConflict) {
			return defaultCodexHomeResult{}, err
		}
	}
	return defaultCodexHomeResult{}, preferences.ErrPreferencesConflict
}
