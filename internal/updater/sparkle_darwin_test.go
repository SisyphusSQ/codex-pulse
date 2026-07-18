//go:build darwin && cgo && sparkle

package updater

import (
	"errors"
	"testing"
)

var _ DownloadAdapter = NewSparkleAdapter()

func TestSparkleIgnoresOnlyTrailingNoUpdateAbort(t *testing.T) {
	if !nativeAbortIgnored(1001, "SUSparkleErrorDomain") {
		t.Fatal("SUNoUpdateError abort was not ignored")
	}
	if nativeAbortIgnored(1000, "SUSparkleErrorDomain") {
		t.Fatal("appcast parse failure was ignored")
	}
	if nativeAbortIgnored(1001, "CodexPulseSparkle") {
		t.Fatal("foreign error with SUNoUpdateError code was ignored")
	}
}

func TestSparkleAdapterRejectsUnconfiguredBundleAndClosesIdempotently(t *testing.T) {
	adapter := NewSparkleAdapter()
	err := adapter.Start(func(Event) {})
	if err == nil {
		t.Fatal("Start succeeded without SUFeedURL and SUPublicEDKey")
	}
	var nativeError *NativeError
	if !errors.As(err, &nativeError) {
		t.Fatalf("Start error=%T %v, want NativeError", err, err)
	}
	if nativeError.Code != FaultConfiguration {
		t.Fatalf("Start code=%q, want %q", nativeError.Code, FaultConfiguration)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
