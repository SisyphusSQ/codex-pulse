package app

import (
	"context"
	"errors"
	"net/http"
	"time"

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

func composeCoreService(
	database *storesqlite.Store,
	preferenceStore *preferences.FileStore,
	queryObserver core.QueryObserver,
	invalidation queryInvalidationNotifier,
) (*core.Service, error) {
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
	grokConfig, err := grokprovider.DefaultConfig()
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	grokCollector, err := grokprovider.NewCollector(repository, grokConfig)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	grokAuth, err := grokprovider.NewAuthReader(grokConfig.AuthPath, time.Now)
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	grokBillingClient, err := grokprovider.NewBillingClient(grokprovider.BillingClientConfig{
		BaseURL: grokConfig.BillingBaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		TokenSource: grokAuth,
	})
	if err != nil {
		return nil, errors.Join(core.ErrService, err)
	}
	grokBillingCollector, err := grokprovider.NewBillingCollector(
		grokBillingClient,
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
		return nil, errors.Join(core.ErrService, err)
	}
	grokService, err := grokprovider.NewQueryService(grokCollector, repository, grokBillingCollector)
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
	return core.NewService(core.ServiceConfig{
		UsageCost: providerRouter, InvocationUsage: providerRouter, PricingCatalog: pricingService,
		RuntimeInfo: runtimeService, QuotaInfo: quotaRouter, QueryObserver: queryObserver,
	})
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
