package lifecycle

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type ConfirmedHome struct {
	Generation int64
	Path       string
	DeviceID   string
	Inode      int64
}

type ConfirmedHomeProvider interface {
	CurrentHome(context.Context) (ConfirmedHome, error)
}

type ReconcileRunner interface {
	RunReconcile(context.Context, ConfirmedHome, ReconcileReason) error
}

type HomeProbe interface {
	Probe(context.Context, string) (logs.HomeMetadata, error)
}

type ConfirmedHomeReconcilerConfig struct {
	HomeProvider ConfirmedHomeProvider
	Runner       ReconcileRunner
	Probe        HomeProbe
}

// ConfirmedHomeReconciler 在轻量reconcile前后都验证逻辑generation与物理
// path/device/inode，避免wake或目录替换期间把旧generation重新开放给worker。
type ConfirmedHomeReconciler struct {
	homeProvider ConfirmedHomeProvider
	runner       ReconcileRunner
	probe        HomeProbe
}

func NewConfirmedHomeReconciler(config ConfirmedHomeReconcilerConfig) (*ConfirmedHomeReconciler, error) {
	if config.HomeProvider == nil || config.Runner == nil {
		return nil, ErrInvalidCoordinator
	}
	if config.Probe == nil {
		config.Probe = logs.NewHomeProbe()
	}
	return &ConfirmedHomeReconciler{
		homeProvider: config.HomeProvider, runner: config.Runner, probe: config.Probe,
	}, nil
}

func (reconciler *ConfirmedHomeReconciler) Reconcile(
	ctx context.Context,
	current store.SchedulerLifecycle,
	reason ReconcileReason,
) (ReconcileResult, error) {
	if reconciler == nil || reconciler.homeProvider == nil || reconciler.runner == nil ||
		reconciler.probe == nil || ctx == nil {
		return ReconcileResult{}, ErrInvalidCoordinator
	}
	home, err := reconciler.homeProvider.CurrentHome(ctx)
	if err != nil {
		return ReconcileResult{}, sanitizedReconcileDependencyError(ctx, err)
	}
	if !validConfirmedHome(home) || home.Generation != current.HomeGeneration {
		return ReconcileResult{}, ErrGenerationChanged
	}
	if err := reconciler.confirmPhysicalHome(ctx, home); err != nil {
		return ReconcileResult{}, err
	}
	if err := reconciler.runner.RunReconcile(ctx, home, reason); err != nil {
		return ReconcileResult{}, sanitizedReconcileDependencyError(ctx, err)
	}
	after, err := reconciler.homeProvider.CurrentHome(ctx)
	if err != nil {
		return ReconcileResult{}, sanitizedReconcileDependencyError(ctx, err)
	}
	if after != home {
		return ReconcileResult{}, ErrGenerationChanged
	}
	if err := reconciler.confirmPhysicalHome(ctx, after); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{
		HomeGeneration: home.Generation, SourceState: store.LifecycleSourceAvailable,
	}, nil
}

func (reconciler *ConfirmedHomeReconciler) confirmPhysicalHome(
	ctx context.Context,
	home ConfirmedHome,
) error {
	metadata, err := reconciler.probe.Probe(ctx, home.Path)
	if err != nil {
		return sanitizedReconcileDependencyError(ctx, err)
	}
	if metadata.Path != home.Path || metadata.DeviceID != home.DeviceID || metadata.Inode != home.Inode {
		return ErrGenerationChanged
	}
	return nil
}

func sanitizedReconcileDependencyError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrSourceUnavailable
}

func validConfirmedHome(home ConfirmedHome) bool {
	return home.Generation >= 0 && filepath.IsAbs(home.Path) && filepath.Clean(home.Path) == home.Path &&
		home.DeviceID != "" && home.Inode > 0
}

var _ Reconciler = (*ConfirmedHomeReconciler)(nil)
