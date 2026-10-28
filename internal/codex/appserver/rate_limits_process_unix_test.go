//go:build !windows

package appserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if string(snapshot.AccountID) != "acct-test-a" || snapshot.RateLimitResetCredits != nil {
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
