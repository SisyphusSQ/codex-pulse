package app

import (
	"errors"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func composeBindingService(
	database *storesqlite.Store,
	preferenceStore *preferences.FileStore,
	queryObserver QueryObserver,
) (*Service, error) {
	if database == nil || preferenceStore == nil {
		return nil, ErrBindingService
	}
	repository := store.NewRepository(database)
	usageService, err := usagecost.NewService(repository)
	if err != nil {
		return nil, errors.Join(ErrBindingService, err)
	}
	quotaService, err := quotaquery.NewCurrentQueryService(repository)
	if err != nil {
		return nil, errors.Join(ErrBindingService, err)
	}
	runtimeService, err := runtimeinfo.NewService(runtimeinfo.Dependencies{
		Quota: quotaService, Runtime: repository, Preferences: preferenceStore,
	})
	if err != nil {
		return nil, errors.Join(ErrBindingService, err)
	}
	return NewService(ServiceConfig{
		UsageCost: usageService, RuntimeInfo: runtimeService, QueryObserver: queryObserver,
	})
}

func openApplicationPreferences() (*preferences.FileStore, error) {
	path, err := preferences.DefaultPath()
	if err != nil {
		return nil, errors.Join(ErrBindingService, err)
	}
	store, err := preferences.NewFileStore(path)
	if err != nil {
		return nil, errors.Join(ErrBindingService, err)
	}
	return store, nil
}

func wailsBindingService(service *Service) application.Service {
	return application.NewServiceWithOptions(service, application.ServiceOptions{
		Name: "QueryService", MarshalError: marshalBindingError,
	})
}
