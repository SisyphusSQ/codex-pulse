package agyprovider

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	defaultAGYQuotaMinRefresh = 2 * time.Minute
	defaultAGYMaxObsPerWindow = 256
)

// QuotaStore 持久化 AGY 配额池快照、观测点及 agy.ls_rpc 来源健康。
type QuotaStore interface {
	ReplaceAGYQuotaSnapshots(context.Context, string, []store.AGYQuotaSnapshot) error
	AGYQuotaSnapshots(context.Context, string) ([]store.AGYQuotaSnapshot, error)
	AppendAGYQuotaObservation(context.Context, store.AGYQuotaObservation, int) error
	AGYQuotaObservations(context.Context, string, string, string) ([]store.AGYQuotaObservation, error)
	UpsertAGYSourceStatus(context.Context, store.CursorSourceStatus) error
}

// QuotaCollectorConfig 控制本机 LS RPC 配额采集器的刷新节流与子开关。
type QuotaCollectorConfig struct {
	MinimumRefresh time.Duration
	Now            func() time.Time
	Enabled        func() bool
	MaxPerWindow   int
}

// OnlineRefreshClass 供 providerrefresh 对 AGY 在线配额组件做统一归类。
type OnlineRefreshClass struct {
	Disabled  bool
	Attempted bool
	Remote    bool
	Err       error
}

// GlobalRefreshReport 汇总 AGY 本地轨迹与本机 LS 配额刷新结果。
type GlobalRefreshReport struct {
	Local struct {
		Attempted bool
		Err       error
	}
	Quota OnlineRefreshClass
}

// QuotaCollector 负责通过本机 LS Connect RPC 采集账号夹心校验后的多配额池快照。
type QuotaCollector struct {
	client *LSClient
	store  QuotaStore
	config QuotaCollectorConfig

	mu                sync.Mutex
	lastAttempt       time.Time
	lastSuccess       time.Time
	lastErr           error
	lastAccountScope  string
	lastAccountEmail  string
	lastAccountPlan   string
	lastAccountSeenAt time.Time
}

func NewQuotaCollector(client *LSClient, storeRepo QuotaStore, config QuotaCollectorConfig) (*QuotaCollector, error) {
	if client == nil || storeRepo == nil || config.MinimumRefresh < 0 {
		return nil, ErrCollector
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxPerWindow <= 0 {
		config.MaxPerWindow = defaultAGYMaxObsPerWindow
	}
	return &QuotaCollector{
		client: client,
		store:  storeRepo,
		config: config,
	}, nil
}

func (c *QuotaCollector) isEnabled() bool {
	if c == nil {
		return false
	}
	if c.config.Enabled == nil {
		return true
	}
	return c.config.Enabled()
}

func (c *QuotaCollector) RefreshIfDue(ctx context.Context, force bool) OnlineRefreshClass {
	if c == nil || ctx == nil {
		return OnlineRefreshClass{Err: ErrCollector}
	}
	if !c.isEnabled() {
		return OnlineRefreshClass{Disabled: true}
	}
	c.mu.Lock()
	now := c.config.Now()
	if !force && !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < c.config.MinimumRefresh {
		c.mu.Unlock()
		return OnlineRefreshClass{Attempted: false}
	}
	c.lastAttempt = now
	c.mu.Unlock()

	atMS := now.UnixMilli()
	result, err := c.client.FetchAccountAndQuota(ctx)
	if err != nil {
		c.recordFailure(ctx, atMS, err)
		return OnlineRefreshClass{Attempted: true, Remote: true, Err: err}
	}

	accountIdentity := result.Status.AccountIdentity()
	accountScope := agyAccountScopeDigest(accountIdentity)
	windows := NormalizeOfficialQuotaPools(result.QuotaSummary, result.Status.ClientModelConfigs, atMS)

	var planPtr *string
	if result.PlanType != "" {
		p := result.PlanType
		planPtr = &p
	}

	snapshots := make([]store.AGYQuotaSnapshot, 0, len(windows))
	for _, w := range windows {
		used := w.UsedPercent
		snapshots = append(snapshots, store.AGYQuotaSnapshot{
			AccountScope:  accountScope,
			Generation:    atMS,
			PlanType:      planPtr,
			LimitID:       w.LimitID,
			LimitName:     w.LimitName,
			WindowKind:    w.WindowKind,
			WindowMinutes: w.WindowMinutes,
			UsedPercent:   &used,
			ResetsAtMS:    cloneInt64Ptr(w.ResetsAtMS),
			DataAsOfMS:    atMS,
			UpdatedAtMS:   atMS,
		})
	}

	if err := providercontrol.WriteWithCommit(ctx, func() error {
		if len(snapshots) > 0 {
			if err := c.store.ReplaceAGYQuotaSnapshots(ctx, accountScope, snapshots); err != nil {
				return err
			}
			for _, w := range windows {
				obs := store.AGYQuotaObservation{
					AccountScope: accountScope,
					LimitID:      w.LimitID,
					WindowKind:   w.WindowKind,
					ObservedAtMS: atMS,
					UsedPercent:  w.UsedPercent,
					ResetsAtMS:   cloneInt64Ptr(w.ResetsAtMS),
				}
				if err := c.store.AppendAGYQuotaObservation(ctx, obs, c.config.MaxPerWindow); err != nil {
					return err
				}
			}
		}
		return c.store.UpsertAGYSourceStatus(ctx, store.CursorSourceStatus{
			Provider:        "agy",
			SourceKey:       SourceLSRPC,
			SourceType:      "ls_rpc",
			State:           "available",
			CoverageState:   "exact",
			CheckpointKind:  "snapshot",
			RowCount:        int64(len(snapshots)),
			LastAttemptAtMS: atMS,
			LastSuccessAtMS: &atMS,
			UpdatedAtMS:     atMS,
		})
	}); err != nil {
		c.recordFailure(ctx, atMS, err)
		return OnlineRefreshClass{Attempted: true, Remote: true, Err: err}
	}

	c.mu.Lock()
	c.lastSuccess = now
	c.lastErr = nil
	c.lastAccountScope = accountScope
	c.lastAccountEmail = result.Status.Email
	c.lastAccountPlan = result.PlanType
	c.lastAccountSeenAt = now
	c.mu.Unlock()

	return OnlineRefreshClass{Attempted: true, Remote: true}
}

func (c *QuotaCollector) recordFailure(ctx context.Context, atMS int64, err error) {
	c.mu.Lock()
	c.lastErr = err
	var lastSuccessPtr *int64
	if !c.lastSuccess.IsZero() {
		ms := c.lastSuccess.UnixMilli()
		lastSuccessPtr = &ms
	}
	c.mu.Unlock()

	code := "read_failed"
	if errors.Is(err, ErrLSNotFound) {
		code = "missing"
	}
	_ = providercontrol.WriteWithCommit(ctx, func() error {
		return c.store.UpsertAGYSourceStatus(ctx, store.CursorSourceStatus{
			Provider:        "agy",
			SourceKey:       SourceLSRPC,
			SourceType:      "ls_rpc",
			State:           "unavailable",
			CoverageState:   "unknown",
			CheckpointKind:  "snapshot",
			LastAttemptAtMS: atMS,
			LastSuccessAtMS: lastSuccessPtr,
			FailureCode:     &code,
			UpdatedAtMS:     atMS,
		})
	})
}

func (c *QuotaCollector) runtimeStatus() (enabled bool, activeScope string, lastAttemptMS *int64, lastSuccessMS *int64, stale bool, failureCode *string) {
	if c == nil {
		return false, "", nil, nil, true, nil
	}
	enabled = c.isEnabled()
	c.mu.Lock()
	defer c.mu.Unlock()
	activeScope = c.lastAccountScope
	if !c.lastAttempt.IsZero() {
		ms := c.lastAttempt.UnixMilli()
		lastAttemptMS = &ms
	}
	if !c.lastSuccess.IsZero() {
		ms := c.lastSuccess.UnixMilli()
		lastSuccessMS = &ms
	}
	if !enabled || c.lastErr != nil {
		stale = true
	}
	if c.lastErr != nil {
		code := "read_failed"
		if errors.Is(c.lastErr, ErrLSNotFound) {
			code = "missing"
		}
		failureCode = &code
	}
	return enabled, activeScope, lastAttemptMS, lastSuccessMS, stale, failureCode
}

func agyAccountScopeDigest(identity string) string {
	return digestString("agy-account:" + identity)
}
