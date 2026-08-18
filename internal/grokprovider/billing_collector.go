package grokprovider

import (
	"context"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type BillingSnapshotWriter interface {
	CommitGrokBillingSnapshot(context.Context, store.GrokBillingSnapshot) error
	RecordGrokBillingFailure(context.Context, int64, string) error
}

type BillingCollectorConfig struct {
	MinimumRefresh time.Duration
	Now            func() time.Time
	Enabled        func() bool
}

type BillingCollector struct {
	client BillingCreditsClient
	writer BillingSnapshotWriter
	config BillingCollectorConfig
	mu     sync.Mutex
	last   time.Time
}

type BillingCreditsClient interface {
	GetCredits(context.Context) (BillingCredits, error)
}

func NewBillingCollector(
	client BillingCreditsClient,
	writer BillingSnapshotWriter,
	config BillingCollectorConfig,
) (*BillingCollector, error) {
	if client == nil || writer == nil {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	if config.Enabled == nil {
		config.Enabled = func() bool { return true }
	}
	return &BillingCollector{client: client, writer: writer, config: config}, nil
}

func (collector *BillingCollector) Refresh(ctx context.Context) error {
	_, err := collector.RefreshIfDue(ctx)
	return err
}

func (collector *BillingCollector) RefreshIfDue(ctx context.Context) (bool, error) {
	if collector == nil || collector.client == nil || collector.writer == nil || ctx == nil {
		return false, ErrCollector
	}
	if !collector.config.Enabled() {
		return false, nil
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < collector.config.MinimumRefresh {
		return false, nil
	}
	atMS := now.UnixMilli()
	credits, err := collector.client.GetCredits(ctx)
	if err != nil {
		_ = collector.writer.RecordGrokBillingFailure(ctx, atMS, billingFailureCode(err))
		collector.last = now
		return true, err
	}
	snapshot := store.GrokBillingSnapshot{
		Generation: atMS, CollectedAtMS: atMS, PeriodType: credits.PeriodType,
		PeriodStartMS: credits.PeriodStartMS, PeriodEndMS: credits.PeriodEndMS,
		UsedPercent: credits.UsedPercent, OnDemandUsed: credits.OnDemandUsed,
		OnDemandCap: credits.OnDemandCap, PrepaidBalance: credits.PrepaidBalance,
		SubscriptionTier: credits.SubscriptionTier, IsUnifiedBilling: credits.IsUnifiedBilling,
		QuotaObservations: []store.GrokBillingQuotaObservation{{
			Generation: atMS, LimitID: grokCreditsLimitID, UsedPercent: credits.UsedPercent,
			CycleStartAtMS: credits.PeriodStartMS, CycleEndAtMS: credits.PeriodEndMS, ObservedAtMS: atMS,
		}},
	}
	if err := collector.writer.CommitGrokBillingSnapshot(ctx, snapshot); err != nil {
		return true, err
	}
	collector.last = now
	return true, nil
}
