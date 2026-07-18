package updater

import (
	"os"
	"strings"
	"testing"
)

func TestSparklePublishesUpdateOnlyAfterRetainingDownloadReply(t *testing.T) {
	data, err := os.ReadFile("sparkle_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	driverStart := strings.Index(source, "showUpdateFoundWithAppcastItem")
	delegateStart := strings.Index(source, "didFindValidUpdate")
	if driverStart < 0 || delegateStart < 0 || delegateStart <= driverStart {
		t.Fatal("Sparkle update callbacks are missing")
	}
	driver := source[driverStart:delegateStart]
	retained := strings.Index(driver, "self.updateChoiceReply = reply")
	published := strings.Index(driver, "cp_sparkle_emit_update_found")
	if retained < 0 || published < 0 || retained >= published {
		t.Fatal("update availability is published before the download reply is retained")
	}
	delegateEnd := strings.Index(source[delegateStart:], "@end")
	if delegateEnd < 0 {
		t.Fatal("Sparkle delegate implementation is incomplete")
	}
	if strings.Contains(source[delegateStart:delegateStart+delegateEnd], "CPEventUpdateFound") {
		t.Fatal("delegate publishes availability before the user-driver reply exists")
	}
}

func TestSparkleMapsRecoveryAndInformationOnlyBeforeReply(t *testing.T) {
	data, err := os.ReadFile("sparkle_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"CPEventResumableUpdateFound", "SPUUserUpdateStageNotDownloaded",
		"item.informationOnlyUpdate", "item.infoURL.absoluteString",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Sparkle recovery/information bridge missing %q", required)
		}
	}
	callbackStart := strings.Index(source, "showUpdateFoundWithAppcastItem")
	callbackEnd := strings.Index(source[callbackStart:], "showUpdateReleaseNotesWithDownloadData")
	callback := source[callbackStart : callbackStart+callbackEnd]
	if strings.Contains(callback, "CPEventInstallStarted") {
		t.Fatal("resumable installing state bypasses shared safe drain")
	}
	downloadStart := strings.Index(source, "int cp_sparkle_download(")
	downloadEnd := strings.Index(source[downloadStart:], "int cp_sparkle_install(")
	download := source[downloadStart : downloadStart+downloadEnd]
	if !strings.Contains(download, "informationOnly") ||
		!strings.Contains(download, "Information-only updates cannot be downloaded") ||
		!strings.Contains(download, "Resumable updates are already ready to install") ||
		!strings.Contains(download, "if (informationOnly || resumable) return;") {
		t.Fatal("native download reply does not reject information-only updates")
	}
	if guard, consume := strings.Index(download, "if (informationOnly || resumable) return;"), strings.Index(download, "reply = holder.userDriver.updateChoiceReply"); guard < 0 || consume < 0 || guard >= consume {
		t.Fatal("native download consumes a resumable reply before rejecting it")
	}
}

func TestSparkleCheckUsesUserDriverFlow(t *testing.T) {
	data, err := os.ReadFile("sparkle_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if !strings.Contains(source, "[holder.updater checkForUpdates]") {
		t.Fatal("Sparkle check does not establish the user-driver choice flow")
	}
	if strings.Contains(source, "[holder.updater checkForUpdateInformation]") {
		t.Fatal("information-only Sparkle check cannot support download confirmation")
	}
}

func TestSparkleUnwrapsSignatureFailuresBeforeMapping(t *testing.T) {
	data, err := os.ReadFile("sparkle_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"cp_sparkle_classification_error", "NSUnderlyingErrorKey", "SUSignatureError", "SUValidationError",
		"CPEventFailed, nil, 0, 0, 0, cp_sparkle_classification_error(error)",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Sparkle failure classification missing %q", required)
		}
	}
}

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
		!strings.Contains(install, "holder.userDriver.resumableUpdate") ||
		!strings.Contains(install, "reply = holder.userDriver.updateChoiceReply") ||
		!strings.Contains(install, "CPEventInstallFailed") ||
		!strings.Contains(install, "CPEventInstallStarted") ||
		strings.Contains(install, "cp_on_main_sync") {
		t.Fatalf("install reply must be extracted and invoked on an asynchronously enqueued main block:\n%s", install)
	}
}
