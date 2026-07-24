package logs

import (
	"reflect"
	"strings"
	"testing"
)

func TestParserVersionIncludesQuotaLimitNames(t *testing.T) {
	t.Parallel()

	if ParserVersion != "codex-rollout-v3" {
		t.Fatalf("ParserVersion = %q, want quota limit name rebuild version", ParserVersion)
	}
}

func TestStreamParserEmitsLocalQuotaObservationsWithoutTokenUsage(t *testing.T) {
	t.Parallel()

	const privateMarker = "PRIVATE_QUOTA_UNKNOWN_FIELD"
	input := []byte(strings.Join([]string{
		quotaSessionMetaLine("session-quota"),
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","limit_name":"通用使用限额","primary":{"used_percent":38.0,"window_minutes":300,"resets_at":1784008800},"secondary":{"used_percent":12.0,"window_minutes":10080,"resets_at":1784595600},"credits":{"private":"` + privateMarker + `"},"plan_type":"pro","future":{"private":"` + privateMarker + `"}}}}`,
	}, "\n") + "\n")

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if result.Stats != (ParseStats{CompleteLines: 2, ParsedLines: 2, EventsEmitted: 3}) {
		t.Fatalf("stats = %#v", result.Stats)
	}
	if len(result.Events) != 3 || result.Events[0].Kind != EventSessionMeta ||
		result.Events[1].Kind != EventQuotaObservation ||
		result.Events[2].Kind != EventQuotaObservation {
		t.Fatalf("events = %#v", result.Events)
	}

	limitID, limitName, planType := "codex", "通用使用限额", "pro"
	want := []QuotaObservationFact{
		{
			SessionID: "session-quota", AccountScope: QuotaAccountScopeDefault,
			Source: QuotaSourceLocalJSONL, LimitID: &limitID, LimitName: &limitName,
			WindowKind:  QuotaWindowPrimary,
			UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 1784008800000,
			PlanType: &planType, ObservedAtMS: 1783990801000, Validity: QuotaValidityAccepted,
		},
		{
			SessionID: "session-quota", AccountScope: QuotaAccountScopeDefault,
			Source: QuotaSourceLocalJSONL, LimitID: &limitID, LimitName: &limitName,
			WindowKind:  QuotaWindowSecondary,
			UsedPercent: 12, WindowMinutes: 10080, ResetsAtMS: 1784595600000,
			PlanType: &planType, ObservedAtMS: 1783990801000, Validity: QuotaValidityAccepted,
		},
	}
	got := []QuotaObservationFact{*result.Events[1].QuotaObservation, *result.Events[2].QuotaObservation}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quota observations = %#v, want %#v", got, want)
	}
	assertMarkerAbsent(t, result, privateMarker)
}

func TestStreamParserClassifiesLocalQuotaCompatibilityAndPartialWindows(t *testing.T) {
	t.Parallel()

	const privateMarker = "PRIVATE_FUTURE_PLAN"
	input := []byte(strings.Join([]string{
		quotaSessionMetaLine("session-compat"),
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":1,"window_minutes":300,"resets_at":1784008800},"plan_type":"` + privateMarker + `"}}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":2,"window_minutes":300,"resets_at":1784008800},"plan_type":null}}}`,
		`{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":3,"window_minutes":300,"resets_at":1783990000},"secondary":{"used_percent":101,"window_minutes":10080,"resets_at":1784595600},"plan_type":"plus"}}}`,
		`{"timestamp":"2026-07-14T01:00:04Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":null,"secondary":null}}}`,
	}, "\n") + "\n")

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(result.Events) != 4 {
		t.Fatalf("events = %#v", result.Events)
	}
	observations := result.Events[1:]
	wantReasons := []QuotaRejectionReason{
		QuotaReasonUnknownPlanType,
		QuotaReasonMissingLimitID,
		QuotaReasonResetNotFuture,
	}
	for index, event := range observations {
		if event.Kind != EventQuotaObservation || event.QuotaObservation == nil {
			t.Fatalf("event %d = %#v", index, event)
		}
		got := event.QuotaObservation
		if got.Validity != QuotaValiditySuspicious || got.RejectionReason == nil ||
			*got.RejectionReason != wantReasons[index] {
			t.Fatalf("event %d observation = %#v", index, got)
		}
	}
	if observations[0].QuotaObservation.PlanType == nil ||
		*observations[0].QuotaObservation.PlanType != QuotaPlanUnknown {
		t.Fatalf("future plan was not canonicalized: %#v", observations[0].QuotaObservation)
	}
	if observations[1].QuotaObservation.LimitID != nil {
		t.Fatalf("missing limit id was guessed: %#v", observations[1].QuotaObservation)
	}
	if len(result.Diagnostics) != 2 ||
		result.Diagnostics[0].Code != DiagnosticInvalidQuotaWindow ||
		result.Diagnostics[1].Code != DiagnosticInvalidQuotaSnapshot {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	assertMarkerAbsent(t, result, privateMarker)
}

func TestStreamParserRejectsStructurallyInvalidQuotaPlanTypes(t *testing.T) {
	t.Parallel()

	longPlan := strings.Repeat("x", maxIdentifierBytes+1)
	inputs := []string{
		`{"private":"object"}`,
		`["array"]`,
		`42`,
		`true`,
		`"` + longPlan + `"`,
	}
	for index, planType := range inputs {
		input := []byte(strings.Join([]string{
			quotaSessionMetaLine("session-invalid-plan"),
			`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":4,"window_minutes":300,"resets_at":1784008800},"plan_type":` + planType + `}}}`,
		}, "\n") + "\n")
		parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
		if err != nil {
			t.Fatalf("case %d NewStreamParser() error = %v", index, err)
		}
		result, err := parser.Feed(0, input)
		if err != nil {
			t.Fatalf("case %d Feed() error = %v", index, err)
		}
		if len(result.Events) != 1 || result.Events[0].Kind != EventSessionMeta ||
			len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != DiagnosticInvalidQuotaSnapshot {
			t.Fatalf("case %d result = %#v, want invalid snapshot without quota observation", index, result)
		}
	}
}

func TestStreamParserKeepsTokenUsageAndQuotaAsIndependentFacts(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		quotaSessionMetaLine("session-usage-quota"),
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10},"last_token_usage":{"input_tokens":4}},"rate_limits":{"limit_id":"codex","primary":{"used_percent":4,"window_minutes":300,"resets_at":1784008800}}}}`,
	}, "\n") + "\n")

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	wantKinds := []EventKind{
		EventSessionMeta, EventTurnStarted, EventSessionUsage, EventTurnUsage, EventQuotaObservation,
	}
	gotKinds := make([]EventKind, 0, len(result.Events))
	for _, event := range result.Events {
		gotKinds = append(gotKinds, event.Kind)
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("event kinds = %#v, want %#v", gotKinds, wantKinds)
	}
}

func TestStreamParserKeepsValidQuotaWhenTokenInfoDrifts(t *testing.T) {
	t.Parallel()

	const privateMarker = "PRIVATE_INVALID_TOKEN_INFO"
	input := []byte(strings.Join([]string{
		quotaSessionMetaLine("session-info-drift"),
		`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":"` + privateMarker + `"}},"rate_limits":{"limit_id":"codex","primary":{"used_percent":4,"window_minutes":300,"resets_at":1784008800}}}}`,
	}, "\n") + "\n")

	parser, err := NewStreamParser(ParserConfig{SourceKind: SourceKindSession})
	if err != nil {
		t.Fatalf("NewStreamParser() error = %v", err)
	}
	result, err := parser.Feed(0, input)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(result.Events) != 2 || result.Events[1].Kind != EventQuotaObservation ||
		result.Events[1].QuotaObservation == nil || len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != DiagnosticInvalidField {
		t.Fatalf("result = %#v, want valid quota plus invalid token info diagnostic", result)
	}
	assertMarkerAbsent(t, result, privateMarker)
}

func quotaSessionMetaLine(sessionID string) string {
	return `{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID + `","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/synthetic","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`
}
