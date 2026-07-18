package main

import (
	"runtime"
	"testing"
	"time"
)

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("probe contract is darwin/arm64 only")
	}
	for _, configuration := range []config{
		{},
		{iconPath: "missing", evidencePath: "evidence", duration: time.Second},
		{iconPath: "missing", evidencePath: "evidence", duration: 3 * time.Minute},
	} {
		if err := run(configuration); err == nil {
			t.Fatalf("run(%#v) unexpectedly succeeded", configuration)
		}
	}
}
