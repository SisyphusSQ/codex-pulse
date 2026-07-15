package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestConfirmedHomeReconcilerValidatesPhysicalIdentityBeforeAndAfterRun(t *testing.T) {
	t.Parallel()

	homePath := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(homePath, directory), 0o700); err != nil {
			t.Fatalf("Mkdir(%s) error = %v", directory, err)
		}
	}
	metadata, err := logs.NewHomeProbe().Probe(context.Background(), homePath)
	if err != nil {
		t.Fatalf("HomeProbe.Probe() error = %v", err)
	}
	provider := &mutableHomeProvider{value: ConfirmedHome{
		Generation: 4, Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
	}}
	runner := &recordingReconcileRunner{}
	reconciler, err := NewConfirmedHomeReconciler(ConfirmedHomeReconcilerConfig{
		HomeProvider: provider, Runner: runner,
	})
	if err != nil {
		t.Fatalf("NewConfirmedHomeReconciler() error = %v", err)
	}
	result, err := reconciler.Reconcile(context.Background(), store.SchedulerLifecycle{
		HomeGeneration: 4,
	}, ReconcileSystemWake)
	if err != nil || result.HomeGeneration != 4 || result.SourceState != store.LifecycleSourceAvailable ||
		len(runner.calls) != 1 || runner.calls[0] != ReconcileSystemWake {
		t.Fatalf("Reconcile() = %#v, %v; calls=%v", result, err, runner.calls)
	}

	runner.after = func() { provider.value.Generation = 5 }
	if _, err := reconciler.Reconcile(context.Background(), store.SchedulerLifecycle{
		HomeGeneration: 4,
	}, ReconcileSourceChange); err == nil {
		t.Fatal("Reconcile(generation drift) error = nil")
	}
}

func TestConfirmedHomeReconcilerDoesNotExposeDependencyErrorText(t *testing.T) {
	t.Parallel()

	privatePath := filepath.Join(string(filepath.Separator), "private", "codex-home", "secret")
	provider := &mutableHomeProvider{value: ConfirmedHome{
		Generation: 4, Path: privatePath, DeviceID: "device", Inode: 1,
	}}
	probeErr := &os.PathError{Op: "open", Path: privatePath, Err: os.ErrPermission}
	reconciler, err := NewConfirmedHomeReconciler(ConfirmedHomeReconcilerConfig{
		HomeProvider: provider, Runner: &recordingReconcileRunner{}, Probe: errorHomeProbe{err: probeErr},
	})
	if err != nil {
		t.Fatalf("NewConfirmedHomeReconciler() error = %v", err)
	}
	_, err = reconciler.Reconcile(context.Background(), store.SchedulerLifecycle{
		HomeGeneration: 4,
	}, ReconcileSystemWake)
	if !errors.Is(err, ErrSourceUnavailable) || strings.Contains(err.Error(), privatePath) ||
		strings.Contains(err.Error(), probeErr.Error()) {
		t.Fatalf("Reconcile(private probe error) = %q, want typed sanitized error", err)
	}
}

type mutableHomeProvider struct {
	value ConfirmedHome
}

func (provider *mutableHomeProvider) CurrentHome(context.Context) (ConfirmedHome, error) {
	return provider.value, nil
}

type recordingReconcileRunner struct {
	calls []ReconcileReason
	after func()
}

type errorHomeProbe struct {
	err error
}

func (probe errorHomeProbe) Probe(context.Context, string) (logs.HomeMetadata, error) {
	return logs.HomeMetadata{}, probe.err
}

func (runner *recordingReconcileRunner) RunReconcile(
	_ context.Context,
	_ ConfirmedHome,
	reason ReconcileReason,
) error {
	runner.calls = append(runner.calls, reason)
	if runner.after != nil {
		runner.after()
	}
	return nil
}
