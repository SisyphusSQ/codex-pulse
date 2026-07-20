package corev1_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 测试 CoreService contract 在迁移场景下暴露完整业务面，并排除桌面平台职责。
func TestCoreProtoExposesExactRPCSurface(t *testing.T) {
	content := readCoreProto(t)
	service := regexp.MustCompile(`(?s)service CoreService\s*\{(.*?)\n\}`).FindStringSubmatch(content)
	if len(service) != 2 {
		t.Fatal("core.proto does not define CoreService")
	}
	matches := regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`).FindAllStringSubmatch(service[1], -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	sort.Strings(got)
	want := []string{
		"AnalyzeSessionIndexRepair", "Bootstrap", "ConfirmHomeSwitch", "Contracts", "DataHealth",
		"Handshake", "Health", "HealthProjection", "Job", "ListHealth", "ListJobs", "ListProjects",
		"ListSessions", "ListSources", "MigrationRecoveryCancel", "MigrationRecoveryConfirm",
		"MigrationRecoveryExit", "MigrationRecoveryPrepare", "MigrationRecoveryRetry",
		"MigrationRecoveryState", "NotifyLifecycle", "PlanHomeSwitch", "ProjectDetail", "QuotaCurrent",
		"RecoverHomeSwitch", "RequestQuotaRefresh", "RunRuntimeAction", "SessionDetail", "Settings",
		"Shutdown", "Source", "SubscribeInvalidations", "UpdateSettings", "UsageCost",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("CoreService RPCs = %v, want %v", got, want)
	}
	for _, forbidden := range []string{
		"CheckForUpdates", "DownloadUpdate", "InstallUpdate", "CancelUpdate", "SkipUpdate", "SnoozeUpdate",
		"Window", "Tray", "Popover",
	} {
		if strings.Contains(service[1], forbidden) {
			t.Fatalf("CoreService unexpectedly exposes desktop concern %q", forbidden)
		}
	}
}

// 测试 NumericValue 在 Protobuf 中独立表达真实零与 unknown，而不是依赖 scalar 默认值。
func TestCoreProtoPreservesPresenceAndContentFreeErrors(t *testing.T) {
	content := readCoreProto(t)
	for _, pattern := range []string{
		`(?s)message NumericValue\s*\{.*optional int64 value\s*=.*optional string unknown_reason\s*=`,
		`(?s)message ResponseMeta\s*\{.*string status\s*=.*repeated Issue issues\s*=`,
		`(?s)message ErrorDetail\s*\{.*string code\s*=.*string message_key\s*=.*optional string field\s*=.*bool retryable\s*=`,
		`(?s)message HandshakeResponse\s*\{.*string contract_version\s*=.*string transport\s*=`,
		`(?s)message QueryInvalidationEvent\s*\{.*string version\s*=.*string domain\s*=.*uint64 sequence\s*=`,
	} {
		if !regexp.MustCompile(pattern).MatchString(content) {
			t.Fatalf("core.proto does not satisfy contract pattern %q", pattern)
		}
	}
	for _, forbidden := range []string{
		"raw_error", "error_message", "stack_trace", "auth_token", "access_token", "refresh_token", "authorization",
	} {
		if regexp.MustCompile(`(?i)\b` + forbidden + `\b`).MatchString(content) {
			t.Fatalf("core.proto exposes forbidden error or credential field %q", forbidden)
		}
	}
}

func readCoreProto(t testing.TB) string {
	t.Helper()
	content, err := os.ReadFile("core.proto")
	if err != nil {
		t.Fatalf("read core.proto: %v", err)
	}
	return string(content)
}
