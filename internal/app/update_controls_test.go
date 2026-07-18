package app

import (
	"context"
	"errors"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

func TestUpdateControlsExposeFiniteStateAndDelegateActions(t *testing.T) {
	t.Parallel()

	version := "42"
	command := &updateBindingStub{view: updater.View{
		Snapshot: updater.Snapshot{Phase: updater.PhaseAvailable, Update: &updater.Update{
			Version: version, DisplayVersion: "0.2.0", ContentLength: 9_007_199_254_740_993,
			Architecture: "arm64", ReleaseNotes: "安全更新", FeedSignatureStatus: updater.SignatureSucceeded,
		}},
		AutoCheckEnabled: true, CheckIntervalSeconds: 3600, PromptVisible: true,
	}}
	service := newUpdateBindingTestService(t, command)

	state, err := service.UpdateState(t.Context())
	if err != nil || state.Phase != string(updater.PhaseAvailable) || state.CurrentVersion != "0.0.0" || state.Version != version ||
		state.ContentLength != "9007199254740993" || !state.PromptVisible || state.FaultCode != "" {
		t.Fatalf("UpdateState=%#v err=%v", state, err)
	}
	trigger, triggerErr := service.CheckForUpdates(t.Context())
	download, downloadErr := service.DownloadUpdate(t.Context())
	cancel, cancelErr := service.CancelUpdate(t.Context())
	skip, skipErr := service.SkipUpdate(t.Context(), version)
	snooze, snoozeErr := service.SnoozeUpdate(t.Context(), 3600)
	if triggerErr != nil || downloadErr != nil || cancelErr != nil || skipErr != nil || snoozeErr != nil ||
		!trigger.Accepted || trigger.Reason != string(updater.TriggerReasonManual) ||
		download.Result != "download_requested" || cancel.Result != "cancel_requested" ||
		skip.Result != "skipped" || snooze.Result != "snoozed" ||
		command.trigger != updater.TriggerManual || command.downloadCalls != 1 || command.cancelCalls != 1 ||
		command.skippedVersion != version || command.snooze != time.Hour {
		t.Fatalf("receipts=%#v/%#v/%#v/%#v/%#v errors=%v/%v/%v/%v/%v command=%#v",
			trigger, download, cancel, skip, snooze, triggerErr, downloadErr, cancelErr, skipErr, snoozeErr, command)
	}
}

func TestUpdateControlsRejectInvalidInputsBeforeDelegation(t *testing.T) {
	t.Parallel()

	command := &updateBindingStub{}
	service := newUpdateBindingTestService(t, command)
	_, skipErr := service.SkipUpdate(t.Context(), "")
	_, snoozeErr := service.SnoozeUpdate(t.Context(), 60)
	assertBindingFailure(t, skipErr, basequery.ErrorValidation, "version", "synthetic-secret-version")
	assertBindingFailure(t, snoozeErr, basequery.ErrorValidation, "seconds", "synthetic-secret-seconds")
	if command.skippedVersion != "" || command.snooze != 0 {
		t.Fatalf("invalid commands reached dependency: %#v", command)
	}
}

func TestUpdateControlsBindExactlyOnce(t *testing.T) {
	t.Parallel()

	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	if _, err := service.UpdateState(t.Context()); err == nil {
		t.Fatal("UpdateState before bind succeeded")
	}
	command := &updateBindingStub{}
	if err := service.bindUpdateControls(command); err != nil {
		t.Fatalf("bindUpdateControls: %v", err)
	}
	if err := service.bindUpdateControls(&updateBindingStub{}); !errors.Is(err, ErrBindingService) {
		t.Fatalf("duplicate bind error=%v", err)
	}
}

func newUpdateBindingTestService(t *testing.T, command updateBindingCommand) *Service {
	t.Helper()
	service, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{}, UpdateControls: command,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

type updateBindingStub struct {
	view           updater.View
	trigger        updater.Trigger
	downloadCalls  int
	cancelCalls    int
	skippedVersion string
	snooze         time.Duration
	err            error
}

func (stub *updateBindingStub) View(context.Context) (updater.View, error) {
	return stub.view, stub.err
}
func (stub *updateBindingStub) Trigger(_ context.Context, trigger updater.Trigger) (updater.TriggerReceipt, error) {
	stub.trigger = trigger
	return updater.TriggerReceipt{Accepted: true, Reason: updater.TriggerReasonManual}, stub.err
}
func (stub *updateBindingStub) Download(context.Context) error { stub.downloadCalls++; return stub.err }
func (stub *updateBindingStub) Cancel(context.Context) error   { stub.cancelCalls++; return stub.err }
func (stub *updateBindingStub) Skip(_ context.Context, version string) error {
	stub.skippedVersion = version
	return stub.err
}
func (stub *updateBindingStub) Snooze(_ context.Context, duration time.Duration) error {
	stub.snooze = duration
	return stub.err
}
