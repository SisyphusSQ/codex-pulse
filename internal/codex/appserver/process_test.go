package appserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// 测试 Finder 的最小 PATH 下仍会选择产品认可的绝对 Codex CLI 候选。（风险复现用例）
func TestResolveCodexInvocationsUsesNodeBackedFallbackWithMinimalPath(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	writeNodeBackedCodex(t, binary, `
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.154.0\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	got, err := resolveCodexInvocations(ProcessOptions{}, []string{binary})
	if err != nil {
		t.Fatalf("resolveCodexInvocations() error = %v", err)
	}
	if len(got) == 0 || got[0].binary != binary {
		t.Fatalf("resolveCodexInvocations() = %#v, want %q first", got, binary)
	}
}

func TestWithInitializedLocalRPCRunsNodeBackedCodexWithMinimalPath(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	writeNodeBackedCodex(t, binary, `
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.154.0\n'
  exit 0
fi
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	confirmedHome := t.TempDir()
	wantHome, err := canonicalConfirmedHome(confirmedHome)
	if err != nil {
		t.Fatal(err)
	}

	observedExit := false
	got, err := withInitializedLocalRPC(
		t.Context(),
		confirmedHome,
		ProcessOptions{CodexBinary: binary, OnExit: func(user, system time.Duration) {
			observedExit = true
			if user < 0 || system < 0 {
				t.Errorf("negative App Server CPU counters: %s / %s", user, system)
			}
		}},
		func(_ context.Context, _ *jsonLineRPC, canonicalHome string) (string, error) {
			return canonicalHome, nil
		},
	)
	if err != nil {
		t.Fatalf("withInitializedLocalRPC() error = %v", err)
	}
	if got != wantHome {
		t.Fatalf("withInitializedLocalRPC() = %q, want %q", got, wantHome)
	}
	if !observedExit {
		t.Fatal("App Server process exit was not observed")
	}
}

func TestNodeBackedCodexFindsNVMNodeOutsideCLIDirectory(t *testing.T) {
	home := t.TempDir()
	nodeDirectory := filepath.Join(home, ".nvm", "versions", "node", "v22.1.0", "bin")
	cliDirectory := filepath.Join(home, ".local", "bin")
	for _, directory := range []string{nodeDirectory, cliDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	node := filepath.Join(nodeDirectory, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\nscript=\"$1\"\nshift\nexec /bin/sh \"$script\" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(cliDirectory, "codex")
	writeNodeBackedCodexScript(t, binary, `
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.150.1\n'
  exit 0
fi
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-nvm","rateLimits":{"primary":{"usedPercent":7,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id" ;;
  esac
done`)
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	snapshot, err := ReadLocalAccountRateLimits(
		t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: binary}, true,
	)
	if err != nil || string(snapshot.AccountID) != "acct-test-nvm" {
		t.Fatalf("NVM Node outside CLI dir: snapshot=%#v, err=%v", snapshot, err)
	}
	badDirectory := filepath.Join(home, "bad-node")
	if err := os.MkdirAll(badDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDirectory, "node"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", badDirectory+":/usr/bin:/bin:/usr/sbin:/sbin")
	snapshot, err = ReadLocalAccountRateLimits(
		t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: binary}, true,
	)
	if err != nil || string(snapshot.AccountID) != "acct-test-nvm" {
		t.Fatalf("fallback from broken Node: snapshot=%#v, err=%v", snapshot, err)
	}
	customRuntime := filepath.Join(home, "custom-node-runtime")
	if err := os.WriteFile(customRuntime, []byte("#!/bin/sh\nscript=\"$1\"\nshift\nexec /bin/sh \"$script\" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_PULSE_NODE_BINARY", customRuntime)
	snapshot, err = ReadLocalAccountRateLimits(
		t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: binary}, true,
	)
	if err != nil || string(snapshot.AccountID) != "acct-test-nvm" {
		t.Fatalf("custom Node executable: snapshot=%#v, err=%v", snapshot, err)
	}
}

func TestNodeBackedCodexMissingConfiguredNodeIsDistinct(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "codex")
	writeNodeBackedCodexScript(t, binary, "exit 0")
	_, err := resolveCodexInvocations(ProcessOptions{
		CodexBinary: binary,
		NodeBinary:  filepath.Join(t.TempDir(), "missing-node"),
	}, nil)
	if !errors.Is(err, ErrNodeRuntimeUnavailable) || strings.Contains(err.Error(), binary) {
		t.Fatalf("missing Node error = %v", err)
	}
}

func TestAppCLIUnsupportedMethodFallsBackToUnversionedWorkingCLI(t *testing.T) {
	directory := t.TempDir()
	appBinary := filepath.Join(directory, "app-codex")
	localBinary := filepath.Join(directory, "local-codex")
	writeCodexVersionScript(t, appBinary, "0.155.0-alpha.9.2", `
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unavailable"}}\n' "$id" ;;
  esac
done`)
	writeCodexVersionScript(t, localBinary, "next", `
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-test-fallback","rateLimits":{"primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id" ;;
  esac
done`)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	snapshot, err := withInitializedLocalRPC(t.Context(), t.TempDir(), ProcessOptions{
		codexCandidatesForTest: []string{appBinary, localBinary},
	}, func(ctx context.Context, rpc *jsonLineRPC, _ string) (AccountRateLimitsSnapshot, error) {
		return readAccountRateLimits(ctx, rpc, true)
	})
	if err != nil || string(snapshot.AccountID) != "acct-test-fallback" {
		t.Fatalf("fallback read = %#v, %v", snapshot, err)
	}
}

func TestMissingAppAccountIdentityDoesNotFallBackToAnotherCLI(t *testing.T) {
	directory := t.TempDir()
	appBinary := filepath.Join(directory, "app-codex")
	localBinary := filepath.Join(directory, "local-codex")
	writeCodexVersionScript(t, appBinary, "0.155.0-alpha.9.2", `
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":null,"rateLimits":{"primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id" ;;
  esac
done`)
	writeCodexVersionScript(t, localBinary, "0.156.0", `
while IFS= read -r line; do
  id="$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"accountId":"acct-stale-local","rateLimits":{"primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1784008800}}}}\n' "$id" ;;
  esac
done`)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	snapshot, err := withInitializedLocalRPC(t.Context(), t.TempDir(), ProcessOptions{
		codexCandidatesForTest: []string{appBinary, localBinary},
	}, func(ctx context.Context, rpc *jsonLineRPC, _ string) (AccountRateLimitsSnapshot, error) {
		return readAccountRateLimits(ctx, rpc, true)
	})
	if !errors.Is(err, ErrAccountIdentityUnavailable) || len(snapshot.AccountID) != 0 {
		t.Fatalf("missing App identity should fail closed: snapshot=%#v, err=%v", snapshot, err)
	}
}

func TestResolveCodexInvocationsKeepsCandidateOrderWithoutVersionGate(t *testing.T) {
	directory := t.TempDir()
	pathDir := filepath.Join(directory, "path")
	localDir := filepath.Join(directory, "local-bin")
	alphaDir := filepath.Join(directory, "ChatGPT.app", "Contents", "Resources")
	if err := os.MkdirAll(pathDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(alphaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPATH := filepath.Join(pathDir, "codex")
	stableLocal := filepath.Join(localDir, "codex")
	alphaApp := filepath.Join(alphaDir, "codex")
	writeCodexVersionScript(t, oldPATH, "0.150.1", "exit 0")
	writeCodexVersionScript(t, stableLocal, "0.154.0", "exit 0")
	writeCodexVersionScript(t, alphaApp, "0.154.0-alpha.6.2", "exit 0")
	t.Setenv("PATH", pathDir)

	got, err := resolveCodexInvocations(ProcessOptions{}, []string{alphaApp, stableLocal})
	if err != nil {
		t.Fatalf("resolveCodexInvocations() error = %v", err)
	}
	if len(got) == 0 || got[0].binary != alphaApp {
		t.Fatalf("resolveCodexInvocations() = %#v, want app %q first", got, alphaApp)
	}
	inspection, inspectErr := InspectCodexBinary(got[0].binary)
	if inspectErr != nil || inspection.Version != "0.154.0-alpha.6.2" ||
		inspection.CapabilityState != CodexCapabilityUnverified {
		t.Fatalf("InspectCodexBinary(%q) = %#v, %v", got[0].binary, inspection, inspectErr)
	}
}

func TestResolveCodexInvocationsAcceptsAlphaExplicitBinary(t *testing.T) {
	directory := t.TempDir()
	alpha := filepath.Join(directory, "codex")
	writeCodexVersionScript(t, alpha, "0.154.0-alpha.6.2", "exit 0")

	got, err := resolveCodexInvocations(ProcessOptions{CodexBinary: alpha}, nil)
	if err != nil || len(got) != 1 || got[0].binary != alpha {
		t.Fatalf("resolveCodexInvocations(alpha) = %#v, %v", got, err)
	}
}

func writeCodexVersionScript(t *testing.T, path, version, body string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  printf 'codex-cli " + version + "\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeNodeBackedCodex(t *testing.T, path, body string) {
	t.Helper()
	directory := filepath.Dir(path)
	node := filepath.Join(directory, "node")
	if err := os.WriteFile(
		node,
		[]byte("#!/bin/sh\nscript=\"$1\"\nshift\nexec /bin/sh \"$script\" \"$@\"\n"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	writeNodeBackedCodexScript(t, path, body)
}

func writeNodeBackedCodexScript(t *testing.T, path, body string) {
	t.Helper()
	script := "#!/usr/bin/env node\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultCodexBinaryCandidatesPreferApp(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	candidates := defaultCodexBinaryCandidates()
	want := "/Applications/ChatGPT.app/Contents/Resources/codex"
	if len(candidates) == 0 || candidates[0] != want {
		t.Fatalf("defaultCodexBinaryCandidates()[0] = %#v, want %q first", candidates, want)
	}
	if !slices.Contains(candidates, filepath.Join(home, ".local", "bin", "codex")) {
		t.Fatalf("local CLI candidate missing: %#v", candidates)
	}
}

func TestIsolatedCodexEnvironmentReplacesInheritedHome(t *testing.T) {
	t.Parallel()

	got := isolatedCodexEnvironment([]string{
		"PATH=/usr/bin",
		"CODEX_HOME=/private/tmp/cp-inherited-home",
		"LANG=zh_CN.UTF-8",
		"CODEX_HOME=/another/private/home",
	}, "/private/tmp/cp-app-smoke.test/runtime/codex-home")
	want := []string{
		"PATH=/usr/bin",
		"LANG=zh_CN.UTF-8",
		"CODEX_HOME=/private/tmp/cp-app-smoke.test/runtime/codex-home",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("isolated environment = %#v, want %#v", got, want)
	}
}
