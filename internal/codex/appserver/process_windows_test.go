//go:build windows

package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if os.Getenv("CODEX_PULSE_TEST_APP_SERVER") == "1" && len(os.Args) > 1 {
		os.Exit(runWindowsCodexFixture(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func runWindowsCodexFixture(arguments []string) int {
	if strings.HasSuffix(arguments[0], ".js") {
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		return 125
	}
	if arguments[0] == "--version" {
		fmt.Println("codex-cli 0.154.0")
		return 0
	}
	if arguments[0] == "--fixture-child" {
		time.Sleep(20 * time.Second)
		return 0
	}
	if arguments[0] != "app-server" {
		return 125
	}
	executable, _ := os.Executable()
	mode := filepath.Base(executable)
	if strings.Contains(mode, "child") {
		child := exec.Command(executable, "--fixture-child")
		child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if child.Start() != nil {
			return 125
		}
		if os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "child-pid"), []byte(fmt.Sprint(child.Process.Pid)), 0o600) != nil {
			return 125
		}
	}
	home := os.Getenv("CODEX_HOME")
	cwd, _ := os.Getwd()
	observation, _ := json.Marshal(struct{ Home, Directory string }{home, cwd})
	_ = os.WriteFile(filepath.Join(home, "fixture-observation.json"), observation, 0o600)
	scanner := bufio.NewScanner(os.Stdin)
	reads := 0
	for scanner.Scan() {
		if strings.Contains(mode, "silent") || strings.Contains(mode, "child") {
			continue
		}
		var request struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		result := `{}`
		switch request.Method {
		case "initialize":
			if strings.Contains(mode, "unsupported") {
				fmt.Printf("{\"id\":%d,\"error\":{\"code\":-32601}}\n", *request.ID)
				continue
			}
			result = `{"userAgent":"codex-cli 0.154.0"}`
		case "account/rateLimits/read":
			reads++
			id := "acct-fixture-a"
			if strings.Contains(mode, "missing") {
				id = ""
			}
			if strings.Contains(mode, "drift") && reads > 1 {
				id = "acct-fixture-b"
			}
			result = fmt.Sprintf(`{"accountId":%q,"rateLimits":{"limitId":"codex","planType":"pro","primary":{"usedPercent":9,"windowDurationMins":300,"resetsAt":2000000000}},"rateLimitResetCredits":{"availableCount":1,"credits":null}}`, id)
		case "account/read":
			result = `{"account":{"type":"chatgpt","email":"fixture@example.com","planType":"pro"}}`
			if strings.Contains(mode, "signed-out") {
				result = `{"account":null}`
			}
		}
		fmt.Printf("{\"id\":%d,\"result\":%s}\n", *request.ID, result)
	}
	return 0
}

func windowsCodexFixture(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("CODEX_PULSE_TEST_APP_SERVER", "1")
	t.Setenv("CODEX_PULSE_CODEX_BINARY", "")
	t.Setenv("CODEX_PULSE_NODE_BINARY", "")
	t.Setenv("PATH", "")
	path := filepath.Join(t.TempDir(), name+" 中文.exe")
	copyWindowsFixture(t, path)
	return path
}
func copyWindowsFixture(t *testing.T, path string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	from, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer from.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	to, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(to, from)
	closeErr := to.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestWindowsCodexAccountHomeAndResetCredits(t *testing.T) {
	binary := windowsCodexFixture(t, "codex")
	path := filepath.Join(t.TempDir(), "Home 中文 path")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	home := confirmedAccountTestHome(t, path, 1)
	result, err := ReadLocalAccountSandwich(t.Context(), home, ProcessOptions{CodexBinary: binary})
	if err != nil || string(result.BeforeID) != "acct-fixture-a" || string(result.AfterID) != "acct-fixture-a" || result.Account == nil || result.Account.PlanType == nil || *result.Account.PlanType != "pro" {
		t.Fatalf("account sandwich failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(path, "fixture-observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observation struct{ Home, Directory string }
	if json.Unmarshal(data, &observation) != nil || !executablePathsEqual(observation.Home, path) || !executablePathsEqual(observation.Directory, path) {
		t.Fatal("child Home or working directory differs")
	}
	quota, err := ReadLocalAccountRateLimits(t.Context(), home, ProcessOptions{CodexBinary: binary}, false)
	if err != nil || quota.RateLimitResetCredits == nil || quota.RateLimitResetCredits.AvailableCount != 1 {
		t.Fatalf("reset count unavailable: %v", err)
	}
	inspected, err := InspectCodexBinary(binary)
	if err != nil || inspected.Version != "0.154.0" || inspected.CapabilityState != CodexCapabilityUnverified {
		t.Fatalf("inspection = %#v %v", inspected, err)
	}
}

func TestWindowsCodexHomePinPreventsLaunchRace(t *testing.T) {
	binary := windowsCodexFixture(t, "codex")
	path := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	home := confirmedAccountTestHome(t, path, 1)
	options := ProcessOptions{CodexBinary: binary, afterBeforeStartForTest: func() error {
		if err := os.Rename(path, path+"-replaced"); err == nil {
			t.Error("Home could be replaced while bound")
		}
		return nil
	}}
	if _, err := ReadLocalAccount(t.Context(), home, options); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLocalAccount(t.Context(), home, ProcessOptions{CodexBinary: binary}); !errors.Is(err, ErrConfirmedHomeChanged) {
		t.Fatalf("stale identity accepted: %v", err)
	}
}

func TestWindowsCodexCancellationReapsEntireJob(t *testing.T) {
	binary := windowsCodexFixture(t, "child")
	home := confirmedAccountTestHome(t, t.TempDir(), 1)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	exited := false
	_, err := ReadLocalAccount(ctx, home, ProcessOptions{CodexBinary: binary, OnExit: func(time.Duration, time.Duration) { exited = true }})
	if !errors.Is(err, context.DeadlineExceeded) || !exited {
		t.Fatalf("cancellation/reap: %v %v", err, exited)
	}
	data, err := os.ReadFile(filepath.Join(home.Path, "child-pid"))
	if err != nil {
		t.Fatal(err)
	}
	var pid uint32
	if _, err := fmt.Sscan(string(data), &pid); err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	state, err := windows.WaitForSingleObject(process, 3000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived job close: %v %v", state, err)
	}
}

func TestWindowsCodexFallbackAndAccountIsolation(t *testing.T) {
	unsupported := windowsCodexFixture(t, "unsupported")
	good := windowsCodexFixture(t, "codex")
	value, err := withInitializedLocalRPC(t.Context(), t.TempDir(), ProcessOptions{codexCandidatesForTest: []string{unsupported, good}}, func(context.Context, *jsonLineRPC, string) (bool, error) { return true, nil })
	if err != nil || !value {
		t.Fatalf("capability fallback failed: %v", err)
	}
	missing := windowsCodexFixture(t, "missing")
	_, err = withInitializedLocalRPC(t.Context(), t.TempDir(), ProcessOptions{codexCandidatesForTest: []string{missing, good}}, func(ctx context.Context, rpc *jsonLineRPC, _ string) (AccountRateLimitsSnapshot, error) {
		return readAccountRateLimits(ctx, rpc, true)
	})
	if !errors.Is(err, ErrAccountIdentityUnavailable) {
		t.Fatalf("missing identity fell through to another installation: %v", err)
	}
	drift := windowsCodexFixture(t, "drift")
	sandwich, err := ReadLocalAccountSandwich(t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: drift})
	if err != nil || string(sandwich.BeforeID) == string(sandwich.AfterID) {
		t.Fatalf("identity drift was hidden: %v", err)
	}
	signedOut := windowsCodexFixture(t, "signed-out")
	account, err := ReadLocalAccount(t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: signedOut})
	if err != nil || account != nil {
		t.Fatalf("signed out account not empty: %v", err)
	}
}

func TestWindowsNpmShimUsesIndependentArguments(t *testing.T) {
	node := windowsCodexFixture(t, "node")
	root := t.TempDir()
	packageRoot := filepath.Join(root, "node_modules", "@openai", "codex")
	if err := os.MkdirAll(filepath.Join(packageRoot, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"@openai/codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "bin", "codex.js"), []byte("// fixture passed as a single independent argument"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(root, "codex.cmd")
	if err := os.WriteFile(shim, []byte("this shim must never be executed & exit 99"), 0o600); err != nil {
		t.Fatal(err)
	}
	invocations, err := prepareCodexInvocations(shim, node)
	if err != nil || len(invocations) != 1 || invocations[0].node != node || invocations[0].binary != filepath.Join(packageRoot, "bin", "codex.js") {
		t.Fatalf("npm resolution: %#v %v", invocations, err)
	}
	_, err = ReadLocalAccount(t.Context(), confirmedAccountTestHome(t, t.TempDir(), 1), ProcessOptions{CodexBinary: shim, NodeBinary: node})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareCodexInvocations(shim, filepath.Join(root, "missing.exe")); !errors.Is(err, ErrNodeRuntimeUnavailable) {
		t.Fatalf("missing Node: %v", err)
	}
	target := filepath.Join(packageRoot, "vendor", "x86_64-pc-windows-msvc", "codex", "codex.exe")
	copyWindowsFixture(t, target)
	invocations, err = prepareCodexInvocations(shim, "")
	if err != nil || len(invocations) != 1 || invocations[0].node != "" || invocations[0].binary != target {
		t.Fatalf("native npm binary not preferred: %#v %v", invocations, err)
	}
}

func TestWindowsEnvironmentKeysAreCaseInsensitive(t *testing.T) {
	environment := isolatedCodexEnvironment([]string{"Path=C:\\Tools", "Codex_Home=C:\\wrong", "CODEX_HOME=C:\\other"}, `C:\confirmed`)
	environment = prependPathEntry(environment, `c:\tools`)
	if len(environment) != 2 || environment[0] != "PATH=C:\\Tools" || environment[1] != "CODEX_HOME=C:\\confirmed" {
		t.Fatalf("environment keys not isolated: %q", environment)
	}
}
