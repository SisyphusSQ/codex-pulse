package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSensitiveAccountIDHasNoStringerOrJSONMarshaler(t *testing.T) {
	t.Parallel()

	var id SensitiveAccountID
	if _, ok := any(id).(interface{ String() string }); ok {
		t.Fatal("SensitiveAccountID must not implement Stringer")
	}
	if _, ok := any(id).(json.Marshaler); ok {
		t.Fatal("SensitiveAccountID must not implement json.Marshaler")
	}
}

func TestNormalizeAccountRateLimitsFromV0154Fixture(t *testing.T) {
	t.Parallel()

	fixtures := loadRateLimitsFixtures(t)

	single, err := NormalizeAccountRateLimits(fixtures["single_bucket"])
	if err != nil {
		t.Fatalf("NormalizeAccountRateLimits(single_bucket) error = %v", err)
	}
	if string(single.AccountID) != "acct-test-a" {
		t.Fatalf("single account id = %q", []byte(single.AccountID))
	}
	if single.OrdinaryUsageAllowed == nil || !*single.OrdinaryUsageAllowed {
		t.Fatalf("single ordinaryUsageAllowed = %#v", single.OrdinaryUsageAllowed)
	}
	if single.RateLimitsByLimitID != nil {
		t.Fatalf("single rateLimitsByLimitId = %#v", single.RateLimitsByLimitID)
	}
	assertWindow(t, single.RateLimits.Primary, 12, 300, 1784008800)
	assertWindow(t, single.RateLimits.Secondary, 40, 10080, 1784613600)
	if single.RateLimits.LimitID == nil || *single.RateLimits.LimitID != "codex" ||
		single.RateLimits.PlanType == nil || *single.RateLimits.PlanType != "pro" ||
		single.RateLimits.SpendControlReached == nil || *single.RateLimits.SpendControlReached {
		t.Fatalf("single rateLimits = %#v", single.RateLimits)
	}
	if single.RateLimitResetCredits == nil || single.RateLimitResetCredits.AvailableCount != 2 ||
		single.RateLimitResetCredits.Credits == nil || len(single.RateLimitResetCredits.Credits) != 2 {
		t.Fatalf("single reset credits = %#v", single.RateLimitResetCredits)
	}
	first := single.RateLimitResetCredits.Credits[0]
	if first.ID != "credit-test-a" || first.Status != "available" || first.ResetType != "codexRateLimits" ||
		first.GrantedAtSeconds != 1781400000 || first.ExpiresAtSeconds == nil || *first.ExpiresAtSeconds != 1784008800 {
		t.Fatalf("first credit = %#v", first)
	}
	if single.RateLimitResetCredits.Credits[1].ExpiresAtSeconds != nil {
		t.Fatalf("second credit expiresAt = %#v", single.RateLimitResetCredits.Credits[1].ExpiresAtSeconds)
	}

	multi, err := NormalizeAccountRateLimits(fixtures["multi_bucket"])
	if err != nil {
		t.Fatalf("NormalizeAccountRateLimits(multi_bucket) error = %v", err)
	}
	if len(multi.RateLimitsByLimitID) != 2 {
		t.Fatalf("multi buckets = %#v", multi.RateLimitsByLimitID)
	}
	if _, ok := multi.RateLimitsByLimitID["codex"]; !ok {
		t.Fatalf("missing codex bucket: %#v", multi.RateLimitsByLimitID)
	}
	if multi.RateLimitResetCredits == nil || multi.RateLimitResetCredits.Credits == nil ||
		len(multi.RateLimitResetCredits.Credits) != 0 {
		t.Fatalf("empty credits array was not preserved: %#v", multi.RateLimitResetCredits)
	}

	creditsNull, err := NormalizeAccountRateLimits(fixtures["credits_null"])
	if err != nil {
		t.Fatalf("NormalizeAccountRateLimits(credits_null) error = %v", err)
	}
	if creditsNull.OrdinaryUsageAllowed != nil {
		t.Fatalf("credits_null ordinaryUsageAllowed = %#v", creditsNull.OrdinaryUsageAllowed)
	}
	if creditsNull.RateLimitResetCredits == nil || creditsNull.RateLimitResetCredits.AvailableCount != 3 ||
		creditsNull.RateLimitResetCredits.Credits != nil {
		t.Fatalf("null credits were not preserved: %#v", creditsNull.RateLimitResetCredits)
	}

	ordinaryNull, err := NormalizeAccountRateLimits(fixtures["ordinary_usage_null"])
	if err != nil {
		t.Fatalf("NormalizeAccountRateLimits(ordinary_usage_null) error = %v", err)
	}
	if ordinaryNull.OrdinaryUsageAllowed != nil {
		t.Fatalf("ordinary_usage_null ordinaryUsageAllowed = %#v", ordinaryNull.OrdinaryUsageAllowed)
	}
	if ordinaryNull.RateLimits.Primary == nil || ordinaryNull.RateLimits.Primary.UsedPercent != 100 ||
		ordinaryNull.RateLimits.Primary.WindowDurationMins != nil ||
		ordinaryNull.RateLimits.Primary.ResetsAtSeconds != nil {
		t.Fatalf("ordinary_usage_null primary = %#v", ordinaryNull.RateLimits.Primary)
	}

	assertNoSensitiveLeak(t)
}

func TestNormalizeAccountRateLimitsRejectsInvalidAccountID(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"null":            `{"accountId":null,"rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}}`,
		"missing":         `{"rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}}`,
		"empty":           `{"accountId":"","rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}}`,
		"padded":          `{"accountId":" acct-test-a ","rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}}`,
		"too_long":        `{"accountId":"` + strings.Repeat("a", 257) + `","rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}}`,
		"invalid_percent": `{"accountId":"acct-test-a","rateLimits":{"primary":{"usedPercent":101,"windowDurationMins":5,"resetsAt":1784008800}}}`,
	}
	for name, raw := range cases {
		raw := raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot, err := NormalizeAccountRateLimits([]byte(raw))
			if err == nil {
				t.Fatalf("NormalizeAccountRateLimits(%s) = %#v", name, snapshot)
			}
			if len(snapshot.AccountID) != 0 {
				t.Fatalf("failed normalize kept account id bytes")
			}
			assertNoSensitiveLeak(t, err)
			if name == "padded" || name == "null" || name == "missing" || name == "empty" || name == "too_long" {
				if !errors.Is(err, ErrAccountIdentityUnavailable) {
					t.Fatalf("NormalizeAccountRateLimits(%s) error = %v, want identity unavailable", name, err)
				}
			}
		})
	}
}

func TestNormalizeAccountRateLimitsRejectsMismatchedLimitID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"accountId":"acct-test-a",
		"rateLimits":{"limitId":"codex","primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}},
		"rateLimitsByLimitId":{
			"codex":{"limitId":"other","primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}}
		}
	}`)
	snapshot, err := NormalizeAccountRateLimits(raw)
	if !errors.Is(err, ErrRateLimitsSchemaIncompatible) {
		t.Fatalf("NormalizeAccountRateLimits(mismatch) = %#v, %v", snapshot, err)
	}
	assertNoSensitiveLeak(t, err, snapshot)
}

func TestNormalizeAccountRateLimitsDoesNotLeakSecretsInErrors(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"accountId":"acct-test-a",
		"rateLimits":{"primary":{"usedPercent":1,"windowDurationMins":5,"resetsAt":1784008800}},
		"rateLimitResetCredits":{"availableCount":1,"credits":[{"id":"credit-test-a","status":"available","resetType":"codexRateLimits","grantedAt":-1}]}
	}`)
	snapshot, err := NormalizeAccountRateLimits(raw)
	if err == nil {
		t.Fatalf("NormalizeAccountRateLimits(invalid credit) = %#v", snapshot)
	}
	assertNoSensitiveLeak(t, err, snapshot)
}

func TestReadAccountRateLimitsRequestsExcludeFlag(t *testing.T) {
	t.Parallel()

	fixtures := loadRateLimitsFixtures(t)
	payload, err := compactJSON(fixtures["single_bucket"])
	if err != nil {
		t.Fatal(err)
	}
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"result":`+payload+`}`+"\n",
	))
	snapshot, err := readAccountRateLimits(context.Background(), rpc, true)
	if err != nil {
		t.Fatalf("readAccountRateLimits() error = %v", err)
	}
	if string(snapshot.AccountID) != "acct-test-a" {
		t.Fatalf("readAccountRateLimits() account = %q", []byte(snapshot.AccountID))
	}
	written := writes.String()
	if !strings.Contains(written, `"method":"account/rateLimits/read"`) ||
		!strings.Contains(written, `"excludeResetCreditDetails":true`) {
		t.Fatalf("account/rateLimits/read request = %q", written)
	}
	assertNoSensitiveLeak(t, err, snapshot, written)
}

func TestReadAccountRateLimitsIdentityUnavailable(t *testing.T) {
	t.Parallel()

	rpc := newJSONLineRPC(
		nopWriteCloser{io.Discard},
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"accountId":null,"rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":5,"resetsAt":1784008800}}}}`+"\n"),
	)
	snapshot, err := readAccountRateLimits(context.Background(), rpc, false)
	if !errors.Is(err, ErrAccountIdentityUnavailable) || len(snapshot.AccountID) != 0 {
		t.Fatalf("readAccountRateLimits(null account) = %#v, %v", snapshot, err)
	}
	assertNoSensitiveLeak(t, err, snapshot)
}

func TestReadAccountRateLimitsUsesConfirmedHome(t *testing.T) {
	confirmedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(confirmedHome, "binding-marker"), []byte("confirmed-home\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	logPath := filepath.Join(directory, "rate-limits.log")
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.154.0\n'
  exit 0
fi
IFS= read -r binding_marker < "$CODEX_HOME/binding-marker"
printf 'home=%s marker=%s\n' "$CODEX_HOME" "$binding_marker" >> "$CODEX_PULSE_RATE_LIMITS_TEST_LOG"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$CODEX_PULSE_RATE_LIMITS_TEST_LOG"
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"account/rateLimits/read"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-a","ordinaryUsageAllowed":null,"rateLimits":{"limitId":"codex","primary":{"usedPercent":9,"windowDurationMins":300,"resetsAt":1784008800}},"rateLimitsByLimitId":null,"rateLimitResetCredits":{"availableCount":1,"credits":null}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_RATE_LIMITS_TEST_LOG", logPath)

	snapshot, err := ReadLocalAccountRateLimits(
		t.Context(),
		confirmedAccountTestHome(t, confirmedHome, 11),
		ProcessOptions{CodexBinary: binary, ClientName: "rate-limits-test", Version: "test"},
		true,
	)
	if err != nil {
		t.Fatalf("ReadLocalAccountRateLimits() error = %v", err)
	}
	if string(snapshot.AccountID) != "acct-test-a" || snapshot.RateLimitResetCredits == nil ||
		snapshot.RateLimitResetCredits.Credits != nil {
		t.Fatalf("ReadLocalAccountRateLimits() = %#v", snapshot)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(content)
	if !strings.Contains(log, "marker=confirmed-home") ||
		!strings.Contains(log, `"method":"account/rateLimits/read"`) ||
		!strings.Contains(log, `"excludeResetCreditDetails":true`) {
		t.Fatalf("App Server rate limits log = %q", log)
	}
	inspection, inspectErr := InspectCodexBinary(binary)
	if inspectErr != nil || inspection.Path != binary || inspection.Version != "0.154.0" ||
		inspection.CapabilityState != CodexCapabilityUnverified {
		t.Fatalf("InspectCodexBinary() = %#v, %v", inspection, inspectErr)
	}
	assertNoSensitiveLeak(t, log)
}

func TestReadAccountRateLimitsScriptedProcessReturnsSequentialIdentities(t *testing.T) {
	confirmedHome := t.TempDir()
	directory := t.TempDir()
	counter := filepath.Join(directory, "count")
	if err := os.WriteFile(counter, []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.154.0\n'
  exit 0
fi
count_file="$CODEX_PULSE_RATE_LIMITS_SEQUENCE"
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"account/rateLimits/read"'*)
      count=$(cat "$count_file")
      count=$((count + 1))
      printf '%s\n' "$count" > "$count_file"
      case "$count" in
        1)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-a","rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id"
          ;;
        2)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"synthetic first B failure"}}\n' "$id"
          ;;
        3)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-b","rateLimits":{"limitId":"codex","primary":{"usedPercent":70,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id"
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-a","rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id"
          ;;
      esac
      ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_RATE_LIMITS_SEQUENCE", counter)
	home := confirmedAccountTestHome(t, confirmedHome, 12)
	options := ProcessOptions{CodexBinary: binary, ClientName: "rate-limits-sequence", Version: "test"}
	first, err := ReadLocalAccountRateLimits(t.Context(), home, options, true)
	if err != nil || string(first.AccountID) != "acct-test-a" {
		t.Fatalf("first A = %#v, %v", first, err)
	}
	second, err := ReadLocalAccountRateLimits(t.Context(), home, options, true)
	if err == nil {
		t.Fatalf("first B failure = %#v, want error", second)
	}
	if strings.Contains(err.Error(), "synthetic first B failure") || strings.Contains(err.Error(), "acct-test-b") {
		t.Fatalf("RPC error leaked server details: %v", err)
	}
	third, err := ReadLocalAccountRateLimits(t.Context(), home, options, true)
	if err != nil || string(third.AccountID) != "acct-test-b" ||
		third.RateLimits.Primary == nil || third.RateLimits.Primary.UsedPercent != 70 {
		t.Fatalf("B success = %#v, %v", third, err)
	}
	fourth, err := ReadLocalAccountRateLimits(t.Context(), home, options, true)
	if err != nil || string(fourth.AccountID) != "acct-test-a" {
		t.Fatalf("restored A = %#v, %v", fourth, err)
	}
	assertNoSensitiveLeak(t, err)
}

func TestRPCErrorOmitsServerMessageAndData(t *testing.T) {
	t.Parallel()

	privateMarker := "private prompt from stderr-like RPC message"
	response := `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"` + privateMarker + `","data":{"accountId":"acct-test-a","creditId":"credit-test-a"}}}` + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(response))
	var result AccountRateLimitsSnapshot
	err := rpc.Call(context.Background(), "account/rateLimits/read", accountRateLimitsReadParams{}, &result)
	var rpcErr RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32603 {
		t.Fatalf("Call() error = %v", err)
	}
	if err.Error() != "App Server RPC error -32603" {
		t.Fatalf("Call() error text = %q", err)
	}
	assertNoSensitiveLeak(t, err, privateMarker)
}

func TestRPCErrorMethodNotFoundIsCapabilityUnavailable(t *testing.T) {
	t.Parallel()

	response := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"account/rateLimits/read is not available"}}` + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(response))
	var result AccountRateLimitsSnapshot
	err := rpc.Call(context.Background(), "account/rateLimits/read", accountRateLimitsReadParams{}, &result)
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("Call() error = %v, want capability unavailable", err)
	}
	if strings.Contains(err.Error(), "account/rateLimits/read is not available") {
		t.Fatalf("RPC error leaked server message: %v", err)
	}
}

func TestRPCMalformedResponseIsProtocolIncompatible(t *testing.T) {
	t.Parallel()
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader("not-json\n"))
	var result json.RawMessage
	err := rpc.Call(context.Background(), "account/rateLimits/read", accountRateLimitsReadParams{}, &result)
	if !errors.Is(err, ErrProtocolIncompatible) {
		t.Fatalf("malformed response error = %v", err)
	}
}

func TestRPCErrorPreservesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(""))
	var result AccountRateLimitsSnapshot
	err := rpc.Call(ctx, "account/rateLimits/read", accountRateLimitsReadParams{}, &result)
	if !errors.Is(err, context.Canceled) || writes.Len() != 0 {
		t.Fatalf("Call() error=%v writes=%q", err, writes.String())
	}
}

func TestRPCErrorPreservesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(""))
	var result AccountRateLimitsSnapshot
	err := rpc.Call(ctx, "account/rateLimits/read", accountRateLimitsReadParams{}, &result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v", err)
	}
}

func compactJSON(raw json.RawMessage) (string, error) {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func loadRateLimitsFixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "rate_limits_v0154.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(content, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func assertWindow(t *testing.T, window *RateLimitWindow, used int32, minutes int64, resetsAt int64) {
	t.Helper()
	if window == nil || window.UsedPercent != used || window.WindowDurationMins == nil ||
		*window.WindowDurationMins != minutes || window.ResetsAtSeconds == nil || *window.ResetsAtSeconds != resetsAt {
		t.Fatalf("window = %#v, want used=%d minutes=%d resetsAt=%d", window, used, minutes, resetsAt)
	}
}

func assertNoSensitiveLeak(t *testing.T, values ...any) {
	t.Helper()
	for _, value := range values {
		text := sensitiveLeakText(value)
		for _, secret := range []string{"acct-test-a", "credit-test-a", "credit-test-b"} {
			if text != "" && strings.Contains(text, secret) {
				t.Fatalf("sensitive value %q leaked through %T: %s", secret, value, text)
			}
		}
	}
}

func sensitiveLeakText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case error:
		if typed == nil {
			return ""
		}
		return typed.Error()
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
