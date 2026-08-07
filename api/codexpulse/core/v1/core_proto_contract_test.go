package corev1_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
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
		"AccountSnapshot", "AnalyzeSessionIndexRepair", "Bootstrap", "ConfirmHomeSwitch", "Contracts", "DataHealth",
		"Handshake", "Health", "HealthProjection", "InvocationUsage", "Job", "ListHealth", "ListJobs", "ListProjects",
		"ListSessions", "ListSources", "MigrationRecoveryCancel", "MigrationRecoveryConfirm",
		"MigrationRecoveryExit", "MigrationRecoveryPrepare", "MigrationRecoveryRetry",
		"MigrationRecoveryState", "NotifyLifecycle", "PlanHomeSwitch", "ProjectDetail", "QuotaCurrent",
		"PricingCatalogCurrent", "QuotaPace", "RecoverHomeSwitch", "RequestQuotaRefresh", "RunRuntimeAction", "SessionDetail", "Settings",
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
		`(?s)message QueryRequest\s*\{.*optional LocalDateRange time_range\s*=\s*4\s*;.*optional UTCTimeRange exact_time_range\s*=\s*5\s*;`,
		`(?s)message UsageModelItem\s*\{.*string dimension_key\s*=.*AttributionValue model\s*=.*UsageTotals totals\s*=\s*3\s*;.*repeated TrendPoint trend\s*=\s*4\s*;`,
		`(?s)message UsageCostRequest\s*\{.*LocalDateRange range\s*=\s*1\s*;.*string granularity\s*=\s*2\s*;.*optional UTCTimeRange exact_range\s*=\s*3\s*;.*bool include_activity_distribution\s*=\s*4\s*;`,
		`(?s)message ProjectDetailRequest\s*\{.*LocalDateRange range\s*=\s*2\s*;.*optional UTCTimeRange exact_range\s*=\s*5\s*;`,
		`(?s)message ActivityMetrics\s*\{.*NumericValue total_tokens\s*=\s*1\s*;.*NumericValue session_count\s*=\s*2\s*;`,
		`(?s)message ActivityTimelinePoint\s*\{.*NumericValue start_at_ms\s*=\s*1\s*;.*NumericValue end_at_ms\s*=\s*2\s*;.*ActivityMetrics metrics\s*=\s*3\s*;`,
		`(?s)message ActivityWeekdayHourPoint\s*\{.*int32 weekday\s*=\s*1\s*;.*int32 hour\s*=\s*2\s*;.*ActivityMetrics metrics\s*=\s*3\s*;`,
		`(?s)message ActivityDistribution\s*\{.*string timeline_granularity\s*=\s*1\s*;.*repeated ActivityTimelinePoint timeline\s*=\s*2\s*;.*repeated ActivityWeekdayHourPoint weekday_hours\s*=\s*3\s*;.*int32 timeline_bucket_minutes\s*=\s*4\s*;`,
		`(?s)message UsageCostResponse\s*\{.*repeated UsageModelItem models\s*=\s*11\s*;.*ActivityDistribution activity_distribution\s*=\s*12\s*;`,
		`(?s)message InvocationUsageRequest\s*\{.*UTCTimeRange range\s*=\s*1\s*;.*string source_class\s*=\s*3\s*;.*int32 top_limit\s*=\s*4\s*;`,
		`(?s)message InvocationUsageResponse\s*\{.*InvocationTotals totals\s*=\s*5\s*;.*repeated ToolUsageItem tools\s*=\s*7\s*;.*repeated SkillUsageItem skills\s*=\s*8\s*;.*InvocationCoverage coverage\s*=\s*9\s*;`,
		`(?s)message ModelReferencePrice\s*\{.*string model_id\s*=\s*1\s*;.*NumericValue input_micros\s*=\s*2\s*;.*NumericValue cached_input_micros\s*=\s*3\s*;.*NumericValue output_micros\s*=\s*4\s*;`,
		`(?s)message PricingCatalogCurrentResponse\s*\{.*NumericValue evaluated_at_ms\s*=\s*2\s*;.*string pricing_version\s*=\s*3\s*;.*string basis\s*=\s*6\s*;.*NumericValue unit_tokens\s*=\s*7\s*;.*optional string source_url\s*=\s*10\s*;.*repeated ModelReferencePrice items\s*=\s*11\s*;`,
		`(?s)message SessionDetailResponse\s*\{.*reserved 11\s*;.*reserved "daily"\s*;.*repeated TrendPoint trend\s*=\s*12\s*;.*string trend_granularity\s*=\s*13\s*;`,
		`(?s)message CodexAccountIdentity\s*\{\s*string type\s*=\s*1\s*;\s*optional string email\s*=\s*2\s*;\s*optional string plan_type\s*=\s*3\s*;\s*\}`,
		`(?s)message AccountSnapshotResponse\s*\{\s*optional CodexAccountIdentity account\s*=\s*1\s*;\s*\}`,
		`(?s)message QuotaPaceForecast\s*\{.*string state\s*=\s*1\s*;.*optional int64 exhaust_at_ms\s*=\s*3\s*;.*optional int64 lead_before_reset_ms\s*=\s*4\s*;`,
		`(?s)message QuotaPaceWindow\s*\{.*optional double pace_delta_pp\s*=\s*10\s*;.*QuotaPaceForecast forecast\s*=\s*11\s*;.*repeated QuotaPaceCycle historical_cycles\s*=\s*14\s*;.*repeated QuotaPaceHistoryBandPoint history_band\s*=\s*15\s*;`,
		`(?s)message QuotaPaceResponse\s*\{\s*ResponseMeta meta\s*=\s*1\s*;\s*CurrentQuotaPace pace\s*=\s*2\s*;\s*\}`,
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

// 测试破坏性 Session 趋势演进永久隔离旧 daily wire tag，避免绕过握手时静默错读。
func TestSessionDetailDescriptorReservesLegacyDailyField(t *testing.T) {
	t.Parallel()

	message := (&corev1.SessionDetailResponse{}).ProtoReflect().Descriptor()
	if !message.ReservedRanges().Has(protoreflect.FieldNumber(11)) {
		t.Fatal("SessionDetailResponse field 11 must remain reserved")
	}
	foundDaily := false
	for index := 0; index < message.ReservedNames().Len(); index++ {
		if message.ReservedNames().Get(index) == protoreflect.Name("daily") {
			foundDaily = true
			break
		}
	}
	if !foundDaily {
		t.Fatal(`SessionDetailResponse name "daily" must remain reserved`)
	}
	trend := message.Fields().ByName("trend")
	granularity := message.Fields().ByName("trend_granularity")
	if trend == nil || trend.Number() != 12 ||
		granularity == nil || granularity.Number() != 13 {
		t.Fatalf(
			"SessionDetailResponse adaptive trend fields = trend:%v granularity:%v",
			trend,
			granularity,
		)
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
