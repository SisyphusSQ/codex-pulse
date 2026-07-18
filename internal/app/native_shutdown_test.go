package app

import (
	"context"
	"testing"
	"time"
)

func TestNativeQuitPreflightDrainsOnceBeforeQuit(t *testing.T) {
	release := make(chan struct{})
	drainStarted := make(chan struct{})
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "instance-wake-admission", Close: func(context.Context) error {
		close(drainStarted)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{}, 1)
	preflight := &nativeQuitPreflight{}
	if err := preflight.Bind(shutdown, func() { quit <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if preflight.ShouldQuit() || preflight.ShouldQuit() {
		t.Fatal("native request must be canceled while shared drain runs")
	}
	select {
	case <-drainStarted:
	case <-time.After(time.Second):
		t.Fatal("shared drain did not start")
	}
	select {
	case <-quit:
		t.Fatal("Quit ran before shared drain completed")
	default:
	}
	close(release)
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("Quit did not run after shared drain")
	}
	if !preflight.ShouldQuit() {
		t.Fatal("terminal shared drain must allow a repeated native quit")
	}
	select {
	case <-quit:
		t.Fatal("Quit ran more than once")
	default:
	}
}

func TestNativeQuitPreflightFailsClosedBeforeBinding(t *testing.T) {
	preflight := &nativeQuitPreflight{}
	if preflight.ShouldQuit() {
		t.Fatal("unbound preflight allowed native shutdown")
	}
}

func TestNativeQuitPreflightAllowsAlreadyClosedSharedShutdown(t *testing.T) {
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{}, 1)
	preflight := &nativeQuitPreflight{}
	if err := preflight.Bind(shutdown, func() { quit <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if !preflight.ShouldQuit() {
		t.Fatal("already closed shared shutdown must allow native termination")
	}
	select {
	case <-quit:
		t.Fatal("already closed preflight must not bounce through a second Quit")
	default:
	}
}

func TestNativeQuitPreflightArbitratesInstallBeforeTermination(t *testing.T) {
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	preflight := &nativeQuitPreflight{}
	if err := preflight.Bind(shutdown, func() {}); err != nil {
		t.Fatal(err)
	}
	if err := preflight.BeginInstall(); err != nil {
		t.Fatal(err)
	}
	if preflight.ShouldQuit() {
		t.Fatal("termination was allowed before native install dispatch")
	}
	if err := preflight.BeginQuit(); err == nil {
		t.Fatal("concurrent tray quit was admitted while install owned terminal intent")
	}
	preflight.MarkInstallReady()
	if !preflight.ShouldQuit() {
		t.Fatal("termination was not allowed after native install dispatch")
	}
}
