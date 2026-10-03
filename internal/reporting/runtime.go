package reporting

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// Runtime has one serialized operation owner. Close cancels and joins its worker
// before closing reporting.db; no process or scheduler survives the owning App.
type Runtime struct {
	state        *State
	source       Source
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	shutdownDone chan struct{}
	wake         chan struct{}
	operations   sync.Mutex
	cycleMu      sync.Mutex
	cycleCancel  context.CancelFunc
	closeOnce    sync.Once
	closeErr     error
	version      string
}

func Start(state *State, source Source) *Runtime {
	return StartWithVersion(state, source, "development")
}
func StartWithVersion(state *State, source Source, version string) *Runtime {
	if version == "" {
		version = "development"
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{version: version, state: state, source: source, ctx: ctx, cancel: cancel, done: make(chan struct{}), shutdownDone: make(chan struct{}), wake: make(chan struct{}, 1)}
	go r.run()
	return r
}
func (r *Runtime) Status(ctx context.Context) (Status, error) {
	if r == nil || r.state == nil {
		return Status{State: "storage_unavailable", IntervalSeconds: DefaultIntervalSeconds}, nil
	}
	return r.state.Status(ctx)
}
func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runtime) stopCycle() {
	r.cycleMu.Lock()
	if r.cycleCancel != nil {
		r.cycleCancel()
	}
	r.cycleMu.Unlock()
}
func (r *Runtime) Pair(ctx context.Context, request PairRequest) (Status, error) {
	if r == nil || r.state == nil {
		return Status{}, ErrUnavailable
	}
	r.stopCycle()
	r.operations.Lock()
	defer r.operations.Unlock()
	if r.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	client, err := NewClient(request.Endpoint, request.AllowHTTP)
	if err != nil {
		return Status{}, err
	}
	defer client.Close()
	paired, err := client.Pair(ctx, request.Code)
	if err != nil {
		return Status{}, err
	}
	cfg, err := r.state.settings(ctx)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	cfg.Endpoint = client.endpoint
	cfg.ClientID = paired.ClientID
	cfg.Credential = paired.Credential
	cfg.AllowHTTP = request.AllowHTTP
	cfg.Enabled = false
	cfg.State = "disabled"
	cfg.LastAttemptAtMS = nil
	cfg.LastSuccessAtMS = nil
	if err := r.state.saveSettings(ctx, cfg); err != nil {
		return Status{}, ErrUnavailable
	}
	return r.state.Status(ctx)
}
func (r *Runtime) Configure(ctx context.Context, request ConfigureRequest) (Status, error) {
	if r == nil || r.state == nil {
		return Status{}, ErrUnavailable
	}
	if request.IntervalSeconds < 15 || request.IntervalSeconds > 3600 || request.HistoryStartAtMS < 0 || request.HistoryStartAtMS > reportingv1.MaxTimestampMS {
		return Status{}, ErrSettings
	}
	r.stopCycle()
	r.operations.Lock()
	defer r.operations.Unlock()
	if r.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	cfg, err := r.state.settings(ctx)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	if request.Enabled && (cfg.Credential == "" || cfg.Endpoint == "") {
		return Status{}, ErrReconnect
	}
	status, err := r.state.Status(ctx)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	if request.HistoryStartAtMS != cfg.HistoryStartAtMS {
		present, err := r.state.hasExport(ctx, cfg.partition())
		if err != nil {
			return Status{}, ErrUnavailable
		}
		if present {
			return Status{}, ErrSettings
		}
	}
	if request.HistoryStartAtMS != cfg.HistoryStartAtMS && status.PendingBatches > 0 && !request.ClearPending {
		return Status{}, ErrPending
	}
	if request.ClearPending {
		if err := r.state.clearPending(ctx, cfg.partition()); err != nil {
			return Status{}, ErrUnavailable
		}
	}
	if request.Enabled || request.HistoryStartAtMS != cfg.HistoryStartAtMS || request.ClearPending {
		if err := r.state.resetCursors(ctx, cfg.partition()); err != nil {
			return Status{}, ErrUnavailable
		}
	}
	cfg.Enabled = request.Enabled
	cfg.IntervalSeconds = request.IntervalSeconds
	cfg.HistoryStartAtMS = request.HistoryStartAtMS
	if !cfg.Enabled {
		cfg.State = "disabled"
	} else if cfg.State != "reconnect_required" && cfg.State != "protocol_rejected" {
		cfg.State = "ready"
	}
	if err := r.state.saveSettings(ctx, cfg); err != nil {
		return Status{}, ErrUnavailable
	}
	r.signal()
	return r.state.Status(ctx)
}
func (r *Runtime) SyncNow(ctx context.Context) (Status, error) {
	if r == nil || r.state == nil {
		return Status{}, ErrUnavailable
	}
	r.operations.Lock()
	defer r.operations.Unlock()
	if r.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	status, err := r.Status(ctx)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	if !status.Enabled {
		return status, ErrSettings
	}
	if status.State == "reconnect_required" {
		return status, ErrReconnect
	}
	if status.State == "protocol_rejected" {
		return status, ErrProtocol
	}
	if err := r.state.resetCursors(ctx, credentialSettings{Endpoint: status.Endpoint, ClientID: status.ClientID}.partition()); err != nil {
		return Status{}, ErrUnavailable
	}
	r.signal()
	return status, nil
}
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || r.state == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.cancel()
		r.stopCycle()
		go func() {
			<-r.done
			r.operations.Lock()
			defer r.operations.Unlock()
			r.closeErr = r.state.Close(context.Background())
			close(r.shutdownDone)
		}()
	})
	select {
	case <-r.shutdownDone:
		return r.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) run() {
	defer close(r.done)
	delay := time.Duration(0)
	retry := time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		r.operations.Lock()
		cfg, err := r.state.settings(r.ctx)
		again := false
		if err == nil && cfg.Enabled && cfg.State != "reconnect_required" && cfg.State != "protocol_rejected" {
			ctx, cancel := context.WithTimeout(r.ctx, 60*time.Second)
			r.cycleMu.Lock()
			r.cycleCancel = cancel
			r.cycleMu.Unlock()
			again, err = r.cycle(ctx, cfg)
			cancel()
			r.cycleMu.Lock()
			r.cycleCancel = nil
			r.cycleMu.Unlock()
		}
		r.operations.Unlock()
		if r.ctx.Err() != nil {
			return
		}
		delay = time.Duration(cfg.IntervalSeconds) * time.Second
		if delay <= 0 {
			delay = time.Duration(DefaultIntervalSeconds) * time.Second
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			delay = retry
			retry = min(retry*2, 5*time.Minute)
		} else {
			retry = time.Second
			if again {
				delay = time.Second
			}
		}
		timer.Reset(delay)
	}
}
func finiteState(err error) string {
	switch {
	case errors.Is(err, ErrReconnect):
		return "reconnect_required"
	case errors.Is(err, ErrProtocol):
		return "protocol_rejected"
	case errors.Is(err, ErrQueueFull):
		return "queue_full"
	case errors.Is(err, store.ErrReportingBudget):
		return "source_budget_exceeded"
	case errors.Is(err, ErrTransport), errors.Is(err, context.DeadlineExceeded):
		return "offline"
	case errors.Is(err, store.ErrReportingSource):
		return "source_unavailable"
	default:
		return "storage_unavailable"
	}
}

// cycle 将来源错误限定在该 Provider，最新额度不依赖 Session 和历史游标。
func (r *Runtime) cycle(ctx context.Context, cfg credentialSettings) (again bool, returnErr error) {
	now := time.Now().UnixMilli()
	cfg.LastAttemptAtMS = &now
	defer func() {
		if errors.Is(returnErr, context.Canceled) || r.ctx.Err() != nil {
			return
		}
		if returnErr != nil {
			cfg.State = finiteState(returnErr)
		}
		if err := r.state.saveSettings(r.ctx, cfg); err != nil {
			returnErr = ErrUnavailable
		}
	}()
	client, err := NewClient(cfg.Endpoint, cfg.AllowHTTP)
	if err != nil {
		return false, err
	}
	defer client.Close()
	drain := func(limit int) error {
		for range limit {
			item, err := r.state.next(ctx, cfg.partition())
			if errors.Is(err, gorm.ErrRecordNotFound) {
				break
			}
			if err != nil {
				return err
			}
			receipt, err := client.Upload(ctx, cfg.Credential, item)
			if err != nil {
				return err
			}
			if err := r.state.Ack(ctx, item, receipt); err != nil {
				return err
			}
			success := time.Now().UnixMilli()
			cfg.LastSuccessAtMS = &success
		}
		return nil
	}
	// 先释放少量队列空间，然后优先排入当前额度；历史不会占满每次上传机会。
	if err := drain(8); err != nil {
		return false, err
	}
	cfg.State = "ready"
	sourceErrors := map[string]error{}
	unavailableProviders := map[string]bool{}
	missing := false
	record := func(provider string, err error) {
		if errors.Is(err, store.ErrReportingSource) {
			missing = true
		}
		if err != nil {
			sourceErrors[provider] = err
			if returnErr == nil {
				returnErr = err
			}
		}
	}
	providers := []string{"codex", "cursor", "grok"}
	if current, ok := r.source.(interface {
		CurrentFacts(context.Context, string, int64) (ExportFactsPage, error)
	}); ok {
		for _, provider := range providers {
			page, err := current.CurrentFacts(ctx, provider, cfg.HistoryStartAtMS)
			if errors.Is(err, store.ErrReportingSource) {
				missing = true
				unavailableProviders[provider] = true
				continue
			}
			if err != nil {
				record(provider, err)
				continue
			}
			_, err = r.state.EnqueueFactGroups(ctx, cfg.partition(), page.Groups, -1)
			record(provider, err)
		}
	}
	for _, provider := range providers {
		more, unavailable, err := r.exportFacts(ctx, cfg, provider, now)
		again = again || more
		missing = missing || unavailable
		unavailableProviders[provider] = unavailableProviders[provider] || unavailable
		record(provider, err)
	}
	for _, provider := range providers {
		more, unavailable, err := r.exportSessions(ctx, cfg, provider, now)
		again = again || more
		missing = missing || unavailable
		unavailableProviders[provider] = unavailableProviders[provider] || unavailable
		record(provider, err)
	}
	if missing {
		cfg.State = "partial"
	}
	status, err := r.state.Status(ctx)
	if err != nil {
		return false, ErrUnavailable
	}
	cursor, err := r.state.cursor(ctx, cfg.partition(), "status", "device")
	if err != nil {
		return false, ErrUnavailable
	}
	if cursor.NextDueAtMS <= now || returnErr != nil || status.FullSyncState == "running" {
		statuses := []reportingv1.DeviceStatus{}
		for _, provider := range providers {
			facts, err := r.source.Status(ctx, provider, cfg.HistoryStartAtMS)
			if err != nil {
				record(provider, err)
				facts = reportingv1.DeviceStatus{Provider: provider, Status: "source_unavailable"}
			}
			if (facts.Status == "source_unavailable" || unavailableProviders[provider] && facts.Status != "disabled") && status.FullSyncState == "running" {
				record(provider, store.ErrReportingSource)
			}
			facts.Version = r.version
			facts.PendingBatches = status.PendingBatches
			facts.SyncState = "ready"
			if facts.Status == "source_unavailable" {
				facts.SyncState = "source_unavailable"
			}
			if err := sourceErrors[provider]; err != nil {
				facts.SyncState = finiteState(err)
			}
			facts.SyncCheckedAtMS = &now
			facts.FullSyncState = status.FullSyncState
			statuses = append(statuses, facts)
		}
		if err := r.state.EnqueueFacts(ctx, cfg.partition(), reportingv1.Batch{Status: statuses}); err != nil {
			return false, err
		}
		cursor.NextDueAtMS = now + cfg.IntervalSeconds*1000
		if err := r.state.saveCursor(ctx, cursor); err != nil {
			return false, ErrUnavailable
		}
	}
	if err := drain(32); err != nil {
		return false, err
	}
	pending, err := r.state.Status(ctx)
	if err != nil {
		return false, ErrUnavailable
	}
	again = again || pending.PendingBatches > 0
	if returnErr == nil && !again {
		if err := r.state.finishFullSync(ctx, cfg.partition()); err != nil {
			return false, ErrUnavailable
		}
		if pending.FullSyncState == "running" {
			again = true
		}
	}
	return again, returnErr
}

func (r *Runtime) exportFacts(ctx context.Context, cfg credentialSettings, provider string, now int64) (bool, bool, error) {
	partition, err := r.source.FactsPartition(ctx, provider)
	if errors.Is(err, store.ErrReportingSource) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	cursor, err := r.state.cursor(ctx, cfg.partition(), "quota:"+provider, partition)
	if err != nil {
		return false, false, ErrUnavailable
	}
	if cursor.NextDueAtMS > now {
		return false, false, nil
	}
	page, err := r.source.Facts(ctx, provider, cursor.After, cfg.HistoryStartAtMS)
	if errors.Is(err, store.ErrReportingSource) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if page.Partition != partition {
		return false, true, nil
	}
	changed, err := r.state.EnqueueFactGroups(ctx, cfg.partition(), page.Groups)
	if err != nil {
		return false, false, err
	}
	if page.Done {
		cursor.After = ""
		cursor.NextDueAtMS = now + cfg.IntervalSeconds*1000
	} else {
		if page.Next == "" || page.Next == cursor.After {
			return false, false, ErrProtocol
		}
		cursor.After = page.Next
		changed = true
	}
	if err := r.state.saveCursor(ctx, cursor); err != nil {
		return false, false, ErrUnavailable
	}
	return changed, false, nil
}
func (r *Runtime) exportSessions(ctx context.Context, cfg credentialSettings, provider string, now int64) (again, missing bool, err error) {
	homeID, err := r.source.Partition(ctx, provider)
	if errors.Is(err, store.ErrReportingSource) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	cursor, err := r.state.cursor(ctx, cfg.partition(), provider, homeID)
	if err != nil {
		return false, false, ErrUnavailable
	}
	if cursor.NextDueAtMS > now {
		return false, false, nil
	}
	page, err := r.source.Page(ctx, provider, cursor.After, cfg.HistoryStartAtMS)
	if errors.Is(err, store.ErrReportingSource) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	cursor.Authority = cursor.Authority && page.Authority
	for _, snap := range page.Sessions {
		if snap.HomeID != homeID {
			return false, false, store.ErrReportingSource
		}
		snap.HistoryStartAtMS = cfg.HistoryStartAtMS
		if err := r.state.Enqueue(ctx, cfg.partition(), cursor.Sweep, snap); err != nil {
			return false, false, err
		}
	}
	if len(page.Sessions) > 0 {
		if page.Next == "" || page.Next == cursor.After {
			return false, false, ErrProtocol
		}
		cursor.After = page.Next
		again = true
	} else {
		if cursor.Authority {
			removed, err := r.state.removed(ctx, cfg.partition(), provider, homeID, cursor.Sweep)
			if err != nil {
				return false, false, err
			}
			for _, snap := range removed {
				snap.CollectedAtMS = now
				if err := r.state.Enqueue(ctx, cfg.partition(), cursor.Sweep, snap); err != nil {
					return false, false, err
				}
			}
			if len(removed) == 128 {
				return true, false, nil
			}
		}
		cursor.After = ""
		cursor.Sweep = uuid.NewString()
		cursor.Authority = true
		cursor.NextDueAtMS = now + cfg.IntervalSeconds*1000
	}
	if err := r.state.saveCursor(ctx, cursor); err != nil {
		return false, false, ErrUnavailable
	}
	return again, false, nil
}
