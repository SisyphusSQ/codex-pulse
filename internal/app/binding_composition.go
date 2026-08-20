package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/query/agentrouter"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/pricingcatalog"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type coreComposition struct {
	service          *core.Service
	apiSubscriptions *apisubscriptions.Service
}

func composeCoreGraph(
	database *storesqlite.Store,
	preferenceStore *preferences.FileStore,
	queryObserver core.QueryObserver,
	invalidation queryInvalidationNotifier,
	apiKeys apisubscriptions.APIKeyProvider,
	apiCredentials *apisubscriptions.SQLiteCredentialStore,
) (*coreComposition, error) {
	if database == nil || preferenceStore == nil {
		return nil, core.ErrService
	}
	repository := store.NewRepository(database)
	usageService, err := usagecost.NewService(repository)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	invocationService, err := invocationusage.NewService(storelight.NewRepository(database))
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorConfig, err := cursorprovider.DefaultConfig()
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorCollector, err := cursorprovider.NewCollector(repository, cursorConfig)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorAuth, err := cursorprovider.NewDesktopAuthReader(cursorConfig.StateDatabase, time.Now)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorDashboardClient, err := cursorprovider.NewDashboardClient(cursorprovider.DashboardClientConfig{
		BaseURL: cursorprovider.DefaultDashboardBaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		TokenSource: cursorAuth,
	})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorDashboardCollector, err := cursorprovider.NewDashboardCollector(
		cursorDashboardClient,
		repository,
		cursorprovider.DashboardCollectorConfig{MinimumRefresh: 5 * time.Minute, Now: time.Now},
	)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorService, err := cursorprovider.NewQueryService(cursorCollector, repository, cursorDashboardCollector)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	cursorService.SetRefreshNotifier(func() {
		notifyQueryInvalidation(invalidation, context.Background(), core.InvalidationIndex)
	})
	grokService, err := composeGrokQueryService(repository, preferenceStore)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	grokService.SetRefreshNotifier(func() {
		notifyQueryInvalidation(invalidation, context.Background(), core.InvalidationIndex)
	})
	providerRouter, err := agentrouter.New(
		usageService, invocationService, cursorService, cursorService, grokService, grokService,
	)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	pricingService, err := pricingcatalog.NewService(repository, time.Now)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	quotaService, err := quotaquery.NewCurrentQueryService(repository)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	runtimeService, err := runtimeinfo.NewService(runtimeinfo.Dependencies{
		Quota: quotaService, Runtime: repository, Preferences: preferenceStore,
		ProviderSources: combinedProviderRefresher{cursor: cursorService, grok: grokService},
	})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	quotaRouter, err := agentrouter.NewQuota(runtimeService, cursorService, grokService)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	if apiKeys == nil {
		apiKeys = emptyAPIKeyProvider{}
	}
	deepSeekClient, err := apisubscriptions.NewDeepSeekClient(apisubscriptions.ClientConfig{Credentials: apiKeys})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	openCodeGoClient, err := apisubscriptions.NewOpenCodeGoClient(apisubscriptions.ClientConfig{Credentials: apiKeys})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	balanceHistory, err := apisubscriptions.NewSQLiteBalanceHistory(database)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	apiSubscriptions, err := apisubscriptions.NewService(apisubscriptions.ServiceConfig{
		DeepSeek: deepSeekClient, OpenCodeGo: openCodeGoClient,
		History: balanceHistory, Location: localReportingLocation(),
	})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	service, err := core.NewService(core.ServiceConfig{
		UsageCost: providerRouter, InvocationUsage: providerRouter, PricingCatalog: pricingService,
		RuntimeInfo: runtimeService, QuotaInfo: quotaRouter, QueryObserver: queryObserver,
		APISubscriptions: apiSubscriptions,
		APICredentials:   apiCredentials,
	})
	if err != nil {
		return nil, err
	}
	return &coreComposition{service: service, apiSubscriptions: apiSubscriptions}, nil
}

func localReportingLocation() *time.Location {
	resolved, err := filepath.EvalSymlinks("/etc/localtime")
	if err == nil {
		const marker = "/zoneinfo/"
		if index := strings.LastIndex(resolved, marker); index >= 0 {
			if location, loadErr := time.LoadLocation(resolved[index+len(marker):]); loadErr == nil {
				return location
			}
		}
	}
	return time.Local
}

type emptyAPIKeyProvider struct{}

func (emptyAPIKeyProvider) APIKey(string) ([]byte, bool) { return nil, false }

func composeGrokQueryService(
	repository *store.Repository,
	preferenceStore *preferences.FileStore,
) (*grokprovider.QueryService, error) {
	disabled, err := grokprovider.NewDisabledQueryService()
	if err != nil {
		return nil, err
	}
	config, err := grokprovider.DefaultConfig()
	if err != nil {
		return disabled, nil
	}
	collector, err := grokprovider.NewCollector(repository, config)
	if err != nil {
		return disabled, nil
	}
	var lastKnownAutoRefresh atomic.Bool
	lastKnownAutoRefresh.Store(true)
	auth, err := grokprovider.NewAuthReader(config.AuthPath, time.Now, grokprovider.AuthReaderConfig{
		RefreshEnabled: func() bool {
			snapshot, loadErr := preferenceStore.LoadPreferences(context.Background())
			if loadErr == nil {
				lastKnownAutoRefresh.Store(snapshot.Online.GrokAutoRefreshEnabled)
			}
			return lastKnownAutoRefresh.Load()
		},
	})
	if err != nil {
		return disabled, nil
	}
	billingClient, err := grokprovider.NewBillingClient(grokprovider.BillingClientConfig{
		BaseURL: config.BillingBaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		TokenSource: auth,
	})
	if err != nil {
		service, queryErr := grokprovider.NewQueryService(collector, repository)
		if queryErr != nil {
			return disabled, nil
		}
		return service, nil
	}
	billingCollector, err := grokprovider.NewBillingCollector(
		billingClient,
		repository,
		grokprovider.BillingCollectorConfig{
			MinimumRefresh: 5 * time.Minute,
			Now:            time.Now,
			Enabled: func() bool {
				snapshot, loadErr := preferenceStore.LoadPreferences(context.Background())
				if loadErr != nil {
					return true
				}
				return snapshot.Online.GrokQuotaEnabled
			},
		},
	)
	if err != nil {
		service, queryErr := grokprovider.NewQueryService(collector, repository)
		if queryErr != nil {
			return disabled, nil
		}
		return service, nil
	}
	service, err := grokprovider.NewQueryService(collector, repository, billingCollector)
	if err != nil {
		return disabled, nil
	}
	return service, nil
}

type combinedProviderRefresher struct {
	cursor runtimeinfo.ProviderSourceRefresher
	grok   runtimeinfo.ProviderSourceRefresher
}

func (refresher combinedProviderRefresher) Refresh(ctx context.Context) error {
	var first error
	if refresher.cursor != nil {
		if err := refresher.cursor.Refresh(ctx); err != nil && first == nil {
			first = err
		}
	}
	if refresher.grok != nil {
		_ = refresher.grok.Refresh(ctx)
	}
	return first
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
