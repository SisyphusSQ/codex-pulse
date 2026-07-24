package app

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/onboarding"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

type defaultCodexHomeConfiguration struct {
	HomePath            string
	TrackerDatabasePath string
	Store               onboarding.Store
	Probe               onboarding.HomeProbe
}

func ensureDefaultCodexHomeConfigured(
	ctx context.Context,
	config defaultCodexHomeConfiguration,
) (bool, error) {
	if config.HomePath == "" {
		return false, nil
	}
	if ctx == nil || config.Store == nil {
		return false, onboarding.ErrInvalidConfiguration
	}
	if _, err := config.Store.Load(ctx); err == nil {
		return false, nil
	} else if !errors.Is(err, preferences.ErrNotConfigured) {
		return false, err
	}
	if config.Probe == nil {
		config.Probe = logs.NewHomeProbe()
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
		return false, err
	}
	state, err := service.Detect(ctx, "")
	if err != nil {
		return false, err
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
			return false, err
		}
		return true, nil
	}
	return false, nil
}
