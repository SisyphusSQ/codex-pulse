package app

import (
	"errors"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func composeCoreService(
	database *storesqlite.Store,
	preferenceStore *preferences.FileStore,
	queryObserver QueryObserver,
) (*Service, error) {
	if database == nil || preferenceStore == nil {
		return nil, core.ErrService
	}
	repository := store.NewRepository(database)
	usageService, err := usagecost.NewService(repository)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	quotaService, err := quotaquery.NewCurrentQueryService(repository)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	runtimeService, err := runtimeinfo.NewService(runtimeinfo.Dependencies{
		Quota: quotaService, Runtime: repository, Preferences: preferenceStore,
	})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	return core.NewService(core.ServiceConfig{
		UsageCost: usageService, RuntimeInfo: runtimeService, QueryObserver: queryObserver,
	})
}

func openApplicationPreferences() (*preferences.FileStore, error) {
	path, err := preferences.DefaultPath()
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	return openApplicationPreferencesAt(path)
}

func openApplicationPreferencesAt(path string) (*preferences.FileStore, error) {
	store, err := preferences.NewFileStore(path)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	return store, nil
}
