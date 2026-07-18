package updater

import (
	"os"
	"strings"
	"testing"
)

func TestSparkleInstallReplyIsEnqueuedWithoutSynchronousLockCycle(t *testing.T) {
	source, err := os.ReadFile("sparkle_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "int cp_sparkle_install(")
	if start < 0 {
		t.Fatal("cp_sparkle_install source boundary missing")
	}
	end := strings.Index(text[start:], "int cp_sparkle_choose(")
	if end < 0 {
		t.Fatal("cp_sparkle_install source boundary missing")
	}
	install := text[start : start+end]
	if !strings.Contains(install, "holder.userDriver.installChoiceReply = nil;") ||
		!strings.Contains(install, "dispatch_async(dispatch_get_main_queue()") ||
		!strings.Contains(install, "CPEventInstallFailed") ||
		!strings.Contains(install, "CPEventInstallStarted") ||
		strings.Contains(install, "cp_on_main_sync") {
		t.Fatalf("install reply must be extracted and invoked on an asynchronously enqueued main block:\n%s", install)
	}
}
