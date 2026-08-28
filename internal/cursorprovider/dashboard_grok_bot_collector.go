package cursorprovider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type GrokBotUsageClient interface {
	GetSandUsageStatus(context.Context) (GrokBotUsageStatus, error)
}

type GrokBotSnapshotWriter interface {
	CommitCursorGrokBotObservation(context.Context, store.CursorGrokBotCommit) error
	RecordCursorGrokBotFailure(context.Context, int64, string) error
}

type GrokBotCollectorConfig struct {
	MinimumRefresh time.Duration
	Now            func() time.Time
}

type GrokBotCollector struct {
	client GrokBotUsageClient
	writer GrokBotSnapshotWriter
	config GrokBotCollectorConfig
	mu     sync.Mutex
	last   time.Time
}

func NewGrokBotCollector(client GrokBotUsageClient, writer GrokBotSnapshotWriter, config GrokBotCollectorConfig) (*GrokBotCollector, error) {
	if client == nil || writer == nil || config.Now == nil || config.MinimumRefresh < 0 {
		return nil, ErrDashboardProtocol
	}
	return &GrokBotCollector{client: client, writer: writer, config: config}, nil
}

func (collector *GrokBotCollector) Refresh(ctx context.Context) error {
	_, err := collector.RefreshIfDue(ctx)
	return err
}

func (collector *GrokBotCollector) RefreshIfDue(ctx context.Context) (bool, error) {
	if collector == nil || ctx == nil {
		return false, ErrDashboardProtocol
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < collector.config.MinimumRefresh {
		return false, nil
	}
	atMS := now.UnixMilli()
	status, err := collector.client.GetSandUsageStatus(ctx)
	if err != nil {
		return true, collector.recordFailure(ctx, atMS, err)
	}
	if status.PeriodStartMS < 0 || status.NextResetAtMS <= status.PeriodStartMS ||
		atMS < status.PeriodStartMS || atMS > status.NextResetAtMS || status.NextResetAtMS <= atMS {
		return true, collector.recordFailure(ctx, atMS, ErrDashboardProtocol)
	}
	commit := store.CursorGrokBotCommit{
		Generation: atMS, CollectedAtMS: atMS, Included: status.Included,
		CycleStartAtMS: status.PeriodStartMS, CycleEndAtMS: status.NextResetAtMS,
	}
	if status.Included {
		if status.UsagePercent == nil {
			return true, collector.recordFailure(ctx, atMS, ErrDashboardProtocol)
		}
		commit.UsedPercent = status.UsagePercent
	}
	if err := collector.writer.CommitCursorGrokBotObservation(ctx, commit); err != nil {
		return true, fmt.Errorf("%w: persist grok bot observation", ErrDashboardProtocol)
	}
	collector.last = now
	return true, nil
}

func (collector *GrokBotCollector) recordFailure(ctx context.Context, atMS int64, cause error) error {
	failureCode := "read_failed"
	if errors.Is(cause, ErrDesktopAuthExpired) || errors.Is(cause, ErrDesktopAuthUnavailable) ||
		errors.Is(cause, ErrDashboardAuthRejected) {
		failureCode = "auth_expired"
	} else if errors.Is(cause, ErrDashboardProtocol) {
		failureCode = "schema_incompatible"
	}
	if err := collector.writer.RecordCursorGrokBotFailure(ctx, atMS, failureCode); err != nil {
		return fmt.Errorf("%w: persist grok bot failure", ErrDashboardProtocol)
	}
	collector.last = collector.config.Now()
	return nil
}
