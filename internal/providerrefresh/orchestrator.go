package providerrefresh

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

const defaultScheduledInterval = 5 * time.Minute

type CodexRefresher interface {
	RefreshProvider(context.Context, string) ProviderResult
}

type CursorRefresher interface {
	RefreshProvider(context.Context, string) ProviderResult
}

type GrokRefresher interface {
	RefreshProvider(context.Context, string) ProviderResult
}

type Config struct {
	Codex        CodexRefresher
	Cursor       CursorRefresher
	Grok         GrokRefresher
	Controller   *providercontrol.Controller
	Invalidation InvalidationNotifier
	Now          func() time.Time
}

type Orchestrator struct {
	codex        CodexRefresher
	cursor       CursorRefresher
	grok         GrokRefresher
	controller   *providercontrol.Controller
	invalidation InvalidationNotifier
	now          func() time.Time

	mu      sync.Mutex
	flight  *refreshFlight
	codexMu sync.Mutex
}

type refreshFlight struct {
	done    chan struct{}
	receipt Receipt
	err     error
}

func New(config Config) (*Orchestrator, error) {
	if config.Cursor == nil || config.Grok == nil {
		return nil, core.ErrService
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Orchestrator{
		codex: config.Codex, cursor: config.Cursor, grok: config.Grok,
		controller: config.Controller, invalidation: config.Invalidation, now: now,
	}, nil
}

func (orchestrator *Orchestrator) BindCodex(refresher CodexRefresher) {
	if orchestrator == nil || refresher == nil {
		return
	}
	orchestrator.codexMu.Lock()
	defer orchestrator.codexMu.Unlock()
	orchestrator.codex = refresher
}

func (orchestrator *Orchestrator) Refresh(ctx context.Context, trigger string) (Receipt, error) {
	if orchestrator == nil {
		return Receipt{}, core.ErrService
	}
	if ctx == nil {
		return Receipt{}, basequery.NewValidationFailure("context", nil)
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !ValidTrigger(trigger) {
		return Receipt{}, basequery.NewValidationFailure("trigger", ErrInvalidTrigger)
	}
	orchestrator.mu.Lock()
	if flight := orchestrator.flight; flight != nil {
		orchestrator.mu.Unlock()
		select {
		case <-ctx.Done():
			return Receipt{}, ctx.Err()
		case <-flight.done:
			return flight.receipt, flight.err
		}
	}
	flight := &refreshFlight{done: make(chan struct{})}
	orchestrator.flight = flight
	orchestrator.mu.Unlock()

	receipt, err := orchestrator.execute(ctx, trigger)
	flight.receipt = receipt
	flight.err = err
	orchestrator.mu.Lock()
	orchestrator.flight = nil
	orchestrator.mu.Unlock()
	close(flight.done)
	return receipt, err
}

func (orchestrator *Orchestrator) execute(ctx context.Context, trigger string) (Receipt, error) {
	providers := make([]ProviderResult, len(providerOrder))
	var wg sync.WaitGroup
	for index, provider := range providerOrder {
		wg.Add(1)
		go func(index int, provider string) {
			defer wg.Done()
			providers[index] = orchestrator.refreshOne(ctx, provider, trigger)
		}(index, provider)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if orchestrator.controller == nil {
		orchestrator.notifyOnce(ctx, providers)
	}
	return Receipt{Trigger: trigger, Providers: providers}, nil
}

func (orchestrator *Orchestrator) refreshOne(ctx context.Context, provider, trigger string) ProviderResult {
	if ctx.Err() != nil {
		return cancelledProvider(provider)
	}
	if orchestrator.controller != nil {
		operation, err := orchestrator.controller.Begin(ctx, provider)
		if err != nil {
			if errors.Is(err, providercontrol.ErrDisabled) {
				return DisabledProvider(provider, ComponentsFor(provider)...)
			}
			return UnavailableProvider(provider, ComponentsFor(provider)...)
		}
		defer operation.Finish()
		result := orchestrator.refreshAdapter(operation.Context(), provider, trigger)
		if !providerRefreshed(result) {
			return result
		}
		finish, commitErr := operation.BeginCommit()
		if commitErr != nil {
			return DisabledProvider(provider, ComponentsFor(provider)...)
		}
		defer finish()
		orchestrator.notifyOnce(operation.Context(), []ProviderResult{result})
		return result
	}
	return orchestrator.refreshAdapter(ctx, provider, trigger)
}

func (orchestrator *Orchestrator) refreshAdapter(ctx context.Context, provider, trigger string) ProviderResult {
	switch provider {
	case agentprovider.Codex:
		orchestrator.codexMu.Lock()
		refresher := orchestrator.codex
		orchestrator.codexMu.Unlock()
		if refresher == nil {
			return UnavailableProvider(agentprovider.Codex, ComponentsFor(agentprovider.Codex)...)
		}
		return refresher.RefreshProvider(ctx, trigger)
	case agentprovider.Cursor:
		if orchestrator.cursor == nil {
			return UnavailableProvider(agentprovider.Cursor, ComponentsFor(agentprovider.Cursor)...)
		}
		return orchestrator.cursor.RefreshProvider(ctx, trigger)
	default:
		if orchestrator.grok == nil {
			return UnavailableProvider(agentprovider.Grok, ComponentsFor(agentprovider.Grok)...)
		}
		return orchestrator.grok.RefreshProvider(ctx, trigger)
	}
}

func providerRefreshed(result ProviderResult) bool {
	for _, component := range result.Components {
		if component.Status == StatusRefreshed {
			return true
		}
	}
	return false
}

func (orchestrator *Orchestrator) notifyOnce(ctx context.Context, providers []ProviderResult) {
	if orchestrator.invalidation == nil || ctx.Err() != nil {
		return
	}
	seen := make(map[core.InvalidationDomain]struct{}, 2)
	var domains []core.InvalidationDomain
	for _, provider := range providers {
		for _, component := range provider.Components {
			if component.Status != StatusRefreshed {
				continue
			}
			domain := domainFor(provider.Provider, component.Component)
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	for _, domain := range domains {
		_ = orchestrator.invalidation.Notify(ctx, domain)
	}
}

func domainFor(_, component string) core.InvalidationDomain {
	switch component {
	case ComponentCodexQuota, ComponentCodexResetCredits, ComponentCursorDashboard,
		ComponentCursorGrokBot, ComponentGrokBilling:
		return core.InvalidationQuota
	default:
		return core.InvalidationIndex
	}
}

func cancelledProvider(provider string) ProviderResult {
	var components []string
	switch provider {
	case agentprovider.Codex:
		components = []string{ComponentCodexLocal, ComponentCodexQuota, ComponentCodexResetCredits}
	case agentprovider.Cursor:
		components = []string{ComponentCursorLocal, ComponentCursorDashboard, ComponentCursorGrokBot}
	default:
		components = []string{ComponentGrokLocal, ComponentGrokBilling}
	}
	results := make([]ComponentResult, 0, len(components))
	for _, component := range components {
		results = append(results, ComponentResult{
			Component: component, Status: StatusFailed, ReasonCode: ReasonCancelled,
		})
	}
	return SummarizeProvider(provider, results)
}

func StartScheduledLoop(ctx context.Context, orchestrator *Orchestrator, interval time.Duration) {
	if orchestrator == nil || ctx == nil {
		return
	}
	if interval <= 0 {
		interval = defaultScheduledInterval
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_, _ = orchestrator.Refresh(ctx, TriggerScheduled)
				timer.Reset(interval)
			}
		}
	}()
}
