package providercontrol

import (
	"context"
	"sync"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

type operationContextKey struct{}

type Controller struct {
	preferences PreferencesReader
	probes      ProbeSet

	mu        sync.Mutex
	sealed    bool
	catalog   uint64
	providers map[string]*providerRuntime
	settled   func()
}

type providerRuntime struct {
	snapshot Snapshot
	root     context.Context
	cancel   context.CancelFunc

	accepting   bool
	ops         int
	opsDone     chan struct{}
	commits     int
	commitsDone chan struct{}
}

type disableDrain struct {
	runtime     *providerRuntime
	target      EffectiveState
	reason      string
	generation  uint64
	opsDone     <-chan struct{}
	commitsDone <-chan struct{}
}

type operation struct {
	controller *Controller
	provider   string
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	stopRoot   func() bool

	finishOnce sync.Once
}

func NewController(reader PreferencesReader, probes ProbeSet) (*Controller, error) {
	if reader == nil {
		return nil, ErrInvalidController
	}
	if probes.Codex == nil || probes.Cursor == nil || probes.Grok == nil {
		return nil, ErrInvalidController
	}
	controller := &Controller{
		preferences: reader, probes: probes,
		providers: make(map[string]*providerRuntime, len(providerOrder)),
	}
	for _, name := range providerOrder {
		root, cancel := context.WithCancel(context.Background())
		cancel()
		controller.providers[name] = &providerRuntime{
			snapshot: Snapshot{
				Provider: name, Intent: preferences.ProviderIntentAuto,
				Discovery: DiscoveryUnchecked, Effective: EffectiveUnavailable,
				ReasonCode: ReasonUnchecked,
			},
			root: root, cancel: cancel, opsDone: closedSignal(), commitsDone: closedSignal(),
		}
	}
	return controller, nil
}

func (controller *Controller) Snapshot(provider string) (Snapshot, error) {
	return controller.ProviderState(provider)
}

func (controller *Controller) SetSettledNotifier(notifier func()) {
	if controller == nil {
		return
	}
	controller.mu.Lock()
	controller.settled = notifier
	controller.mu.Unlock()
}

func (controller *Controller) ProviderState(provider string) (Snapshot, error) {
	if controller == nil {
		return Snapshot{}, ErrInvalidController
	}
	name, err := NormalizeProvider(provider)
	if err != nil {
		return Snapshot{}, err
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	runtime := controller.providers[name]
	if runtime == nil {
		return Snapshot{}, ErrInvalidProvider
	}
	return runtime.snapshot, nil
}

func (controller *Controller) EnabledProviders() []string {
	if controller == nil {
		return nil
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	enabled := make([]string, 0, len(providerOrder))
	for _, name := range providerOrder {
		if controller.providers[name].snapshot.Effective == EffectiveEnabled {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

func (controller *Controller) Generation() uint64 {
	if controller == nil {
		return 0
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.catalog
}

func (controller *Controller) Snapshots() []Snapshot {
	if controller == nil {
		return nil
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	result := make([]Snapshot, 0, len(providerOrder))
	for _, name := range providerOrder {
		result = append(result, controller.providers[name].snapshot)
	}
	return result
}

func (controller *Controller) RefreshDiscovery(ctx context.Context, _ string) ([]Snapshot, error) {
	if controller == nil {
		return nil, ErrInvalidController
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	intents, homes, err := controller.loadIntents(ctx)
	if err != nil {
		return nil, err
	}
	probes := map[string]ProbeResult{
		agentprovider.Codex:  controller.probes.Codex(ctx, homes),
		agentprovider.Cursor: controller.probes.Cursor(ctx),
		agentprovider.Grok:   controller.probes.Grok(ctx),
	}
	var drains []disableDrain
	controller.mu.Lock()
	if controller.sealed {
		controller.mu.Unlock()
		return nil, ErrSealed
	}
	for _, name := range providerOrder {
		runtime := controller.providers[name]
		intent := intentOf(intents, name)
		probe := probes[name]
		if runtime.snapshot.Effective == EffectiveDisabling {
			continue
		}
		previous := runtime.snapshot
		nextEffective, reason := effectiveForProbe(intent, probe)
		if previous.Effective == EffectiveEnabled && nextEffective != EffectiveEnabled {
			drains = append(drains, controller.prepareDisableLocked(
				runtime, intent, probe.State, EffectiveUnavailable, reason,
			))
			continue
		}
		if previous.Effective != EffectiveEnabled && nextEffective == EffectiveEnabled {
			controller.enableLocked(runtime, intent, probe.State, reason)
			continue
		}
		controller.updateSnapshotLocked(runtime, intent, probe.State, nextEffective, reason)
	}
	controller.mu.Unlock()
	for _, drain := range drains {
		if err := waitDrain(ctx, drain); err != nil {
			controller.continueDrain(drain)
		} else {
			controller.finishDisable(drain)
		}
	}
	return controller.Snapshots(), nil
}

func (controller *Controller) Apply(ctx context.Context, update preferences.ProviderPreferences) (TransitionResult, error) {
	if controller == nil {
		return TransitionResult{}, ErrInvalidController
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TransitionResult{}, err
	}
	if !preferencesIntentValid(update) {
		return TransitionResult{}, ErrInvalidProvider
	}
	_, homes, err := controller.loadIntents(ctx)
	if err != nil {
		return TransitionResult{}, err
	}
	probes := map[string]ProbeResult{
		agentprovider.Codex:  controller.probes.Codex(ctx, homes),
		agentprovider.Cursor: controller.probes.Cursor(ctx),
		agentprovider.Grok:   controller.probes.Grok(ctx),
	}
	intents := map[string]preferences.ProviderIntent{
		agentprovider.Codex:  update.Codex.Intent,
		agentprovider.Cursor: update.Cursor.Intent,
		agentprovider.Grok:   update.Grok.Intent,
	}
	var drains []disableDrain
	reconcilePending := false
	controller.mu.Lock()
	if controller.sealed {
		controller.mu.Unlock()
		return TransitionResult{}, ErrSealed
	}
	for _, name := range providerOrder {
		runtime := controller.providers[name]
		intent := intents[name]
		probe := probes[name]
		if runtime.snapshot.Effective == EffectiveDisabling {
			reconcilePending = true
			continue
		}
		nextEffective, reason := effectiveForProbe(intent, probe)
		if runtime.snapshot.Effective == EffectiveEnabled && nextEffective != EffectiveEnabled {
			target := nextEffective
			if intent == preferences.ProviderIntentDisabled {
				target = EffectiveDisabled
			}
			drains = append(drains, controller.prepareDisableLocked(
				runtime, intent, probe.State, target, reason,
			))
			continue
		}
		if nextEffective == EffectiveEnabled && runtime.snapshot.Effective != EffectiveEnabled {
			controller.enableLocked(runtime, intent, probe.State, reason)
			continue
		}
		controller.updateSnapshotLocked(runtime, intent, probe.State, nextEffective, reason)
	}
	controller.mu.Unlock()
	result := TransitionResult{Applied: !reconcilePending, ReconcileRequired: reconcilePending}
	for _, drain := range drains {
		if err := waitDrain(ctx, drain); err != nil {
			result.Applied = false
			result.ReconcileRequired = true
			controller.continueDrain(drain)
			continue
		}
		controller.finishDisable(drain)
	}
	result.Snapshots = controller.Snapshots()
	return result, nil
}

func (controller *Controller) Begin(ctx context.Context, provider string) (Operation, error) {
	if controller == nil {
		return nil, ErrInvalidController
	}
	if ctx == nil {
		return nil, ErrInvalidController
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := NormalizeProvider(provider)
	if err != nil {
		return nil, err
	}
	controller.mu.Lock()
	if controller.sealed {
		controller.mu.Unlock()
		return nil, ErrSealed
	}
	runtime := controller.providers[name]
	if runtime == nil || !runtime.accepting || runtime.snapshot.Effective != EffectiveEnabled ||
		runtime.root == nil || runtime.root.Err() != nil {
		snapshot := Snapshot{}
		if runtime != nil {
			snapshot = runtime.snapshot
		}
		controller.mu.Unlock()
		if failure := QueryFailure(snapshot); failure != nil {
			return nil, failure
		}
		return nil, ErrUnavailable
	}
	if runtime.ops == 0 {
		runtime.opsDone = make(chan struct{})
	}
	runtime.ops++
	generation := runtime.snapshot.Generation
	root := runtime.root
	controller.mu.Unlock()

	operationContext, cancel := context.WithCancel(ctx)
	stopRoot := context.AfterFunc(root, cancel)
	op := &operation{
		controller: controller, provider: name, generation: generation,
		cancel: cancel, stopRoot: stopRoot,
	}
	op.ctx = context.WithValue(operationContext, operationContextKey{}, op)
	return op, nil
}

func (controller *Controller) Seal(ctx context.Context) error {
	if controller == nil {
		return ErrInvalidController
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var drains []disableDrain
	controller.mu.Lock()
	controller.sealed = true
	for _, name := range providerOrder {
		runtime := controller.providers[name]
		if runtime.accepting || runtime.snapshot.Effective == EffectiveEnabled ||
			runtime.snapshot.Effective == EffectiveDisabling {
			drains = append(drains, controller.prepareDisableLocked(
				runtime, runtime.snapshot.Intent, runtime.snapshot.Discovery,
				EffectiveDisabled, ReasonDisabled,
			))
		}
	}
	controller.mu.Unlock()
	for _, drain := range drains {
		if err := waitDrain(ctx, drain); err != nil {
			controller.continueDrain(drain)
			continue
		}
		controller.finishDisable(drain)
	}
	return ctx.Err()
}

func (op *operation) Context() context.Context {
	if op == nil {
		return context.Background()
	}
	return op.ctx
}

func (op *operation) Generation() uint64 {
	if op == nil {
		return 0
	}
	return op.generation
}

func (op *operation) Provider() string {
	if op == nil {
		return ""
	}
	return op.provider
}

func (op *operation) BeginCommit() (func(), error) {
	if op == nil || op.controller == nil {
		return nil, ErrInvalidController
	}
	controller := op.controller
	controller.mu.Lock()
	runtime := controller.providers[op.provider]
	if runtime == nil || runtime.snapshot.Generation != op.generation || !runtime.accepting ||
		runtime.snapshot.Effective != EffectiveEnabled {
		controller.mu.Unlock()
		return nil, ErrStaleGeneration
	}
	if runtime.commits == 0 {
		runtime.commitsDone = make(chan struct{})
	}
	runtime.commits++
	controller.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			controller.mu.Lock()
			defer controller.mu.Unlock()
			runtime := controller.providers[op.provider]
			if runtime == nil {
				return
			}
			runtime.commits--
			if runtime.commits == 0 {
				close(runtime.commitsDone)
			}
		})
	}, nil
}

func (op *operation) Finish() {
	if op == nil {
		return
	}
	op.finishOnce.Do(func() {
		if op.stopRoot != nil {
			op.stopRoot()
		}
		if op.cancel != nil {
			op.cancel()
		}
		op.controller.mu.Lock()
		defer op.controller.mu.Unlock()
		runtime := op.controller.providers[op.provider]
		if runtime == nil {
			return
		}
		runtime.ops--
		if runtime.ops == 0 {
			close(runtime.opsDone)
		}
	})
}

func OperationFromContext(ctx context.Context) (Operation, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(operationContextKey{}).(*operation)
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func Commit(ctx context.Context) (func(), error) {
	operation, ok := OperationFromContext(ctx)
	if !ok {
		return func() {}, nil
	}
	return operation.BeginCommit()
}

func WriteWithCommit(ctx context.Context, write func() error) error {
	if write == nil {
		return nil
	}
	finish, err := Commit(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return write()
}

func (controller *Controller) loadIntents(
	ctx context.Context,
) (preferences.ProviderPreferences, *preferences.CodexHomePreferences, error) {
	snapshot, err := controller.preferences.LoadPreferences(ctx)
	if err != nil {
		return preferences.ProviderPreferences{}, nil, err
	}
	return snapshot.Providers, snapshot.CodexHome, nil
}

func (controller *Controller) prepareDisableLocked(
	runtime *providerRuntime,
	intent preferences.ProviderIntent,
	discovery DiscoveryState,
	target EffectiveState,
	reason string,
) disableDrain {
	runtime.accepting = false
	runtime.snapshot.Intent = intent
	runtime.snapshot.Discovery = discovery
	runtime.snapshot.Effective = EffectiveDisabling
	runtime.snapshot.ReasonCode = ReasonDisabling
	runtime.snapshot.Generation++
	controller.catalog++
	if runtime.cancel != nil {
		runtime.cancel()
	}
	root, cancel := context.WithCancel(context.Background())
	cancel()
	runtime.root = root
	runtime.cancel = cancel
	return disableDrain{
		runtime: runtime, target: target, reason: reason,
		generation: runtime.snapshot.Generation,
		opsDone:    runtime.opsDone, commitsDone: runtime.commitsDone,
	}
}

func (controller *Controller) enableLocked(
	runtime *providerRuntime,
	intent preferences.ProviderIntent,
	discovery DiscoveryState,
	reason string,
) {
	if runtime.cancel != nil {
		runtime.cancel()
	}
	root, cancel := context.WithCancel(context.Background())
	runtime.root = root
	runtime.cancel = cancel
	runtime.accepting = true
	runtime.snapshot.Intent = intent
	runtime.snapshot.Discovery = discovery
	runtime.snapshot.Effective = EffectiveEnabled
	runtime.snapshot.ReasonCode = reason
	runtime.snapshot.Generation++
	controller.catalog++
	if runtime.ops == 0 {
		runtime.opsDone = closedSignal()
	}
	if runtime.commits == 0 {
		runtime.commitsDone = closedSignal()
	}
}

func (controller *Controller) finishDisable(drain disableDrain) bool {
	controller.mu.Lock()
	runtime := drain.runtime
	if runtime == nil || runtime.snapshot.Effective != EffectiveDisabling ||
		runtime.snapshot.Generation != drain.generation {
		controller.mu.Unlock()
		return false
	}
	reason := drain.reason
	if drain.target == EffectiveDisabled {
		reason = ReasonDisabled
	} else if reason == "" || reason == ReasonDisabling {
		reason = string(runtime.snapshot.Discovery)
		if reason == "" {
			reason = ReasonUnchecked
		}
	}
	runtime.snapshot.Effective = drain.target
	runtime.snapshot.ReasonCode = reason
	runtime.snapshot.Generation++
	controller.catalog++
	controller.mu.Unlock()
	return true
}

func (controller *Controller) continueDrain(drain disableDrain) {
	go func() {
		_ = waitDrain(context.Background(), drain)
		if controller.finishDisable(drain) {
			controller.mu.Lock()
			notifier := controller.settled
			controller.mu.Unlock()
			if notifier != nil {
				notifier()
			}
		}
	}()
}

func (controller *Controller) updateSnapshotLocked(
	runtime *providerRuntime,
	intent preferences.ProviderIntent,
	discovery DiscoveryState,
	effective EffectiveState,
	reason string,
) {
	if runtime.snapshot.Intent != intent || runtime.snapshot.Discovery != discovery ||
		runtime.snapshot.Effective != effective || runtime.snapshot.ReasonCode != reason {
		runtime.snapshot.Generation++
		controller.catalog++
	}
	runtime.snapshot.Intent = intent
	runtime.snapshot.Discovery = discovery
	runtime.snapshot.Effective = effective
	runtime.snapshot.ReasonCode = reason
}

func (controller *Controller) snapshotsLocked() []Snapshot {
	result := make([]Snapshot, 0, len(providerOrder))
	for _, name := range providerOrder {
		result = append(result, controller.providers[name].snapshot)
	}
	return result
}

func waitDrain(ctx context.Context, drain disableDrain) error {
	if err := waitSignal(ctx, drain.commitsDone); err != nil {
		return err
	}
	return waitSignal(ctx, drain.opsDone)
}

func waitSignal(ctx context.Context, signal <-chan struct{}) error {
	if signal == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-signal:
		return nil
	}
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func intentOf(value preferences.ProviderPreferences, provider string) preferences.ProviderIntent {
	switch provider {
	case agentprovider.Codex:
		return value.Codex.Intent
	case agentprovider.Cursor:
		return value.Cursor.Intent
	default:
		return value.Grok.Intent
	}
}

func preferencesIntentValid(value preferences.ProviderPreferences) bool {
	return validIntent(value.Codex.Intent) && validIntent(value.Cursor.Intent) && validIntent(value.Grok.Intent)
}

func validIntent(value preferences.ProviderIntent) bool {
	return value == preferences.ProviderIntentAuto ||
		value == preferences.ProviderIntentEnabled ||
		value == preferences.ProviderIntentDisabled
}

func effectiveForProbe(
	intent preferences.ProviderIntent,
	probe ProbeResult,
) (EffectiveState, string) {
	effective, reason := EffectiveFor(intent, probe.State, false)
	if effective == EffectiveUnavailable && probe.ReasonCode != "" {
		reason = probe.ReasonCode
	}
	return effective, reason
}
