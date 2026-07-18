package app

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestApplicationShutdownCoordinatorClosesInOrderOnce(t *testing.T) {
	var order []string
	var calls atomic.Int32
	coordinator, err := newApplicationShutdownCoordinator(
		shutdownComponent{Name: "admission", Close: func(context.Context) error { order = append(order, "admission"); calls.Add(1); return nil }},
		shutdownComponent{Name: "sqlite", Close: func(context.Context) error { order = append(order, "sqlite"); calls.Add(1); return nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !reflect.DeepEqual(order, []string{"admission", "sqlite"}) {
		t.Fatalf("calls=%d order=%v", calls.Load(), order)
	}
	if snapshot := coordinator.Snapshot(); snapshot.Phase != shutdownPhaseClosed || snapshot.Stage != "closed" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func TestApplicationShutdownCoordinatorTimeoutKeepsDrainingAndCanBeReawaited(t *testing.T) {
	release := make(chan struct{})
	coordinator, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "writer", Close: func(context.Context) error {
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := coordinator.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close timeout=%v", err)
	}
	if snapshot := coordinator.Snapshot(); snapshot.Phase != shutdownPhaseDraining || snapshot.Stage != "writer" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	close(release)
	if err := coordinator.Close(t.Context()); err != nil {
		t.Fatalf("Close retry=%v", err)
	}
}

func TestApplicationShutdownCoordinatorPublishesDrainingBeforeWaiting(t *testing.T) {
	release := make(chan struct{})
	coordinator, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "writer", Close: func(context.Context) error {
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := coordinator.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close canceled=%v", err)
	}
	if snapshot := coordinator.Snapshot(); snapshot.Phase != shutdownPhaseDraining || snapshot.Stage != "writer" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	close(release)
	if err := coordinator.Close(t.Context()); err != nil {
		t.Fatalf("Close retry=%v", err)
	}
}

func TestApplicationShutdownCoordinatorContinuesAfterFailure(t *testing.T) {
	want := errors.New("boom")
	var later atomic.Bool
	coordinator, err := newApplicationShutdownCoordinator(
		shutdownComponent{Name: "first", Close: func(context.Context) error { return want }},
		shutdownComponent{Name: "later", Close: func(context.Context) error { later.Store(true); return nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Close=%v", err)
	}
	if !later.Load() || coordinator.Snapshot().FailedStage != "first" {
		t.Fatalf("later=%v snapshot=%#v", later.Load(), coordinator.Snapshot())
	}
}
