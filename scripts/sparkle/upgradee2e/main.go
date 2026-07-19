// Command upgradee2e executes a local-only, synthetic-key Sparkle upgrade
// matrix. It never publishes artifacts or reads a real update credential.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	controlVersion = "upgrade-e2e-v1"
	sourceVersion  = "0.1.0"
	targetVersion  = "0.2.0"
	sourceBuild    = "100"
	targetBuild    = "200"
	targetSchema   = 15
	sourceSchema   = targetSchema - 1
	markerSession  = "upgrade-e2e-preserved-session"
)

type options struct {
	repository string
	scenario   string
	keep       bool
}

type control struct {
	Version        string `json:"version"`
	Root           string `json:"root"`
	DataDirectory  string `json:"dataDirectory"`
	RuntimeDir     string `json:"runtimeDirectory"`
	Preferences    string `json:"preferences"`
	Evidence       string `json:"evidence"`
	Result         string `json:"result"`
	Scenario       string `json:"scenario"`
	Action         string `json:"action"`
	SourceVersion  string `json:"sourceVersion"`
	TargetVersion  string `json:"targetVersion"`
	MarkerSession  string `json:"markerSession"`
	ExpectedSchema int    `json:"expectedSchema"`
}

type result struct {
	Version       string `json:"version"`
	Scenario      string `json:"scenario"`
	Application   string `json:"applicationVersion"`
	Stage         string `json:"stage"`
	Phase         string `json:"phase"`
	FaultCode     string `json:"faultCode"`
	SchemaVersion int    `json:"schemaVersion"`
	MarkerPresent bool   `json:"markerPresent"`
	BackupPresent bool   `json:"backupPresent"`
	PID           int    `json:"pid"`
}

type binaries struct {
	source        string
	target        string
	failingTarget string
}

type fixtureProcess struct {
	helper     *os.Process
	helperDone chan struct{}
	helperErr  error
	appPID     int
	executable string
}

type userStateGuard struct {
	home                  string
	appcastCacheEntries   map[string]struct{}
	appcastCacheWasAbsent bool
	bundleIDs             map[string]struct{}
	manageDefaults        bool
}

func main() {
	var configured options
	flag.StringVar(&configured.repository, "repo", "", "absolute repository root; defaults to current directory")
	flag.StringVar(&configured.scenario, "scenario", "all", "all, success, bad_signature, offline, information_only, or migration_failure")
	flag.BoolVar(&configured.keep, "keep", false, "retain successful isolated directories")
	flag.Parse()
	if err := run(context.Background(), configured); err != nil {
		fmt.Fprintln(os.Stderr, "upgradee2e:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configured options) (runErr error) {
	if err := requireUpgradeE2EOptIn(); err != nil {
		return err
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return errors.New("requires macOS arm64")
	}
	userState, err := newUserStateGuard()
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, userState.cleanup()) }()
	repository, err := resolveRepository(configured.repository)
	if err != nil {
		return err
	}
	scenarios, err := selectedScenarios(configured.scenario)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "codex-pulse-upgrade-e2e.")
	if err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	completed := false
	defer func() {
		if completed && !configured.keep {
			runErr = errors.Join(runErr, os.RemoveAll(root))
		} else {
			fmt.Fprintln(os.Stderr, "upgradee2e evidence retained:", root)
		}
	}()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	seed := base64.StdEncoding.EncodeToString(private.Seed())
	publicKey := base64.StdEncoding.EncodeToString(public)
	compiled, err := compileFixtures(ctx, repository, root)
	if err != nil {
		return err
	}
	for _, scenario := range scenarios {
		userState.bundleIDs[fixtureBundleIdentifier(filepath.Join(root, scenario))] = struct{}{}
		if err := runScenario(ctx, repository, root, scenario, publicKey, seed, compiled); err != nil {
			return fmt.Errorf("%s: %w", scenario, err)
		}
		fmt.Println("PASS", scenario)
	}
	if err := userState.cleanupAndVerify(); err != nil {
		return err
	}
	completed = true
	return nil
}

func requireUpgradeE2EOptIn() error {
	if os.Getenv("CODEX_PULSE_RUN_UPGRADE_E2E") != "1" {
		return errors.New("set CODEX_PULSE_RUN_UPGRADE_E2E=1 to run the real upgrade matrix")
	}
	return nil
}

func newUserStateGuard() (*userStateGuard, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	guard := &userStateGuard{home: home, bundleIDs: make(map[string]struct{}), manageDefaults: true}
	if err := guard.cleanupSyntheticApplicationState(); err != nil {
		return nil, err
	}
	cache := filepath.Join(home, "Library", "Caches", "Sparkle_generate_appcast")
	guard.appcastCacheEntries, guard.appcastCacheWasAbsent, err = directoryEntrySet(cache)
	if err != nil {
		return nil, err
	}
	return guard, nil
}

func (guard *userStateGuard) cleanup() error {
	if guard == nil {
		return nil
	}
	if err := guard.cleanupSyntheticApplicationState(); err != nil {
		return err
	}
	cache := filepath.Join(guard.home, "Library", "Caches", "Sparkle_generate_appcast")
	entries, _, err := directoryEntrySet(cache)
	if err != nil {
		return err
	}
	for name := range entries {
		if _, existed := guard.appcastCacheEntries[name]; existed {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cache, name)); err != nil {
			return err
		}
	}
	if guard.appcastCacheWasAbsent {
		_ = os.Remove(cache)
	}
	return nil
}

func (guard *userStateGuard) cleanupAndVerify() error {
	for attempt := 0; attempt < 4; attempt++ {
		if err := guard.cleanup(); err != nil {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	for _, directory := range syntheticStateDirectories(guard.home) {
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "com.sisyphussq.codexpulse.upgradee2e.") {
				return fmt.Errorf("synthetic user state remains in %s", directory)
			}
		}
	}
	cache := filepath.Join(guard.home, "Library", "Caches", "Sparkle_generate_appcast")
	after, absent, err := directoryEntrySet(cache)
	if err != nil {
		return err
	}
	if absent != guard.appcastCacheWasAbsent || !sameEntrySet(after, guard.appcastCacheEntries) {
		return errors.New("generate_appcast cache did not return to its pre-run snapshot")
	}
	return nil
}

func (guard *userStateGuard) cleanupSyntheticApplicationState() error {
	if guard.bundleIDs == nil {
		guard.bundleIDs = make(map[string]struct{})
	}
	if guard.manageDefaults {
		for bundleID := range guard.bundleIDs {
			_ = exec.Command("defaults", "delete", bundleID).Run()
		}
	}
	for _, directory := range syntheticStateDirectories(guard.home) {
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "com.sisyphussq.codexpulse.upgradee2e.") {
				continue
			}
			if directory == filepath.Join(guard.home, "Library", "Preferences") {
				bundleID := strings.TrimSuffix(entry.Name(), ".plist")
				if guard.manageDefaults {
					_ = exec.Command("defaults", "delete", bundleID).Run()
				}
				guard.bundleIDs[bundleID] = struct{}{}
			}
			if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func syntheticStateDirectories(home string) []string {
	library := filepath.Join(home, "Library")
	return []string{
		filepath.Join(library, "Caches"), filepath.Join(library, "Preferences"),
		filepath.Join(library, "Saved Application State"), filepath.Join(library, "HTTPStorages"),
		filepath.Join(library, "WebKit"), filepath.Join(library, "Application Support"),
		filepath.Join(library, "Containers"), filepath.Join(library, "Application Scripts"),
	}
}

func directoryEntrySet(path string) (map[string]struct{}, bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]struct{}{}, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	result := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		result[entry.Name()] = struct{}{}
	}
	return result, false, nil
}

func sameEntrySet(first, second map[string]struct{}) bool {
	if len(first) != len(second) {
		return false
	}
	for name := range first {
		if _, ok := second[name]; !ok {
			return false
		}
	}
	return true
}

func resolveRepository(value string) (string, error) {
	if value == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", err
		}
		value = current
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(absolute, "go.mod")); err != nil {
		return "", errors.New("repository root has no go.mod")
	}
	return absolute, nil
}

func selectedScenarios(value string) ([]string, error) {
	if value == "all" {
		return []string{"success", "bad_signature", "offline", "information_only", "migration_failure"}, nil
	}
	for _, allowed := range []string{"success", "bad_signature", "offline", "information_only", "migration_failure"} {
		if value == allowed {
			return []string{value}, nil
		}
	}
	return nil, errors.New("invalid scenario")
}

func compileFixtures(ctx context.Context, repository, root string) (binaries, error) {
	if err := command(ctx, repository, nil, "npm", "--prefix", "frontend", "run", "build"); err != nil {
		return binaries{}, err
	}
	if err := command(ctx, repository, nil, filepath.Join(repository, "scripts/sparkle/prepare_framework.sh"), filepath.Join(repository, ".cache/sparkle")); err != nil {
		return binaries{}, err
	}
	if err := command(ctx, repository, nil, filepath.Join(repository, "scripts/sparkle/prepare_release_tools.sh"), filepath.Join(repository, ".cache/sparkle")); err != nil {
		return binaries{}, err
	}
	if err := command(ctx, repository, nil, "go", "run", "./build/darwin", "source", "docs/design/front/assets/icons"); err != nil {
		return binaries{}, err
	}
	if err := command(ctx, repository, nil, filepath.Join(repository, "build/darwin/generate_icns.sh"), "docs/design/front/assets/icons", "bin/.packaging/icons.icns"); err != nil {
		return binaries{}, err
	}
	if err := command(ctx, repository, nil, "go", "run", "./build/darwin", "render", "docs/design/front/assets/icons", "bin/.packaging/tray"); err != nil {
		return binaries{}, err
	}
	output := filepath.Join(root, "compiled")
	if err := os.MkdirAll(output, 0o700); err != nil {
		return binaries{}, err
	}
	values := binaries{
		source: filepath.Join(output, "source"), target: filepath.Join(output, "target"),
		failingTarget: filepath.Join(output, "target-migration-failure"),
	}
	if err := buildBinary(ctx, repository, values.source, sourceVersion, strconv.Itoa(sourceSchema), ""); err != nil {
		return binaries{}, err
	}
	if err := buildBinary(ctx, repository, values.target, targetVersion, "", ""); err != nil {
		return binaries{}, err
	}
	if err := buildBinary(ctx, repository, values.failingTarget, targetVersion, "", strconv.Itoa(targetSchema)); err != nil {
		return binaries{}, err
	}
	return values, nil
}

func buildBinary(ctx context.Context, repository, output, version, schemaLimit, failingMigration string) error {
	ldflags := "-w -s -X github.com/SisyphusSQ/codex-pulse/internal/app.applicationVersion=" + version
	if schemaLimit != "" {
		ldflags += " -X github.com/SisyphusSQ/codex-pulse/internal/store.upgradeE2ESchemaLimit=" + schemaLimit
	}
	if failingMigration != "" {
		ldflags += " -X github.com/SisyphusSQ/codex-pulse/internal/store.upgradeE2EFailMigration=" + failingMigration
	}
	environment := append(os.Environ(),
		"GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=1", "CGO_CFLAGS=-mmacosx-version-min=15.0",
		"CGO_LDFLAGS=-mmacosx-version-min=15.0", "MACOSX_DEPLOYMENT_TARGET=15.0",
	)
	if err := command(ctx, repository, environment, "go", "build", "-tags", "production sparkle upgradee2e", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", output, "."); err != nil {
		return err
	}
	return command(ctx, repository, nil, filepath.Join(repository, "build/darwin/ensure_rpath.sh"), output, "@executable_path/../Frameworks")
}

func runScenario(ctx context.Context, repository, root, scenario, publicKey, seed string, compiled binaries) (runErr error) {
	scenarioRoot := filepath.Join(root, scenario)
	for _, path := range []string{"data", "runtime", "feed", "install", "saved", "assets", "home"} {
		if err := os.MkdirAll(filepath.Join(scenarioRoot, path), 0o700); err != nil {
			return err
		}
	}
	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(scenarioRoot, "feed"))))
	feedURL := server.URL + "/appcast.xml"
	if scenario == "offline" {
		server.Close()
		closedURL, err := closedLoopbackURL()
		if err != nil {
			return err
		}
		feedURL = closedURL + "/appcast.xml"
	} else {
		defer server.Close()
	}

	targetBinary := compiled.target
	if scenario == "migration_failure" {
		targetBinary = compiled.failingTarget
	}
	targetWorkspace := filepath.Join(scenarioRoot, "target-work")
	targetBundle, err := assembleFixture(ctx, repository, scenarioRoot, targetWorkspace, targetBinary, targetVersion, targetBuild, feedURL, publicKey)
	if err != nil {
		return err
	}
	archive := filepath.Join(scenarioRoot, "feed", "Codex-Pulse-"+targetVersion+"-arm64.zip")
	if err := command(ctx, targetWorkspace, nil, "ditto", "-c", "-k", "--keepParent", targetBundle, archive); err != nil {
		return err
	}
	notes := strings.TrimSuffix(archive, ".zip") + ".txt"
	if err := os.WriteFile(notes, []byte("Synthetic loopback N-1 upgrade evidence.\n"), 0o600); err != nil {
		return err
	}
	if scenario != "offline" {
		if err := generateAppcast(ctx, repository, scenarioRoot, server.URL, seed); err != nil {
			return err
		}
		if scenario == "bad_signature" {
			file, err := os.OpenFile(archive, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, writeErr := file.Write([]byte("tampered-after-signing"))
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				return err
			}
		} else if scenario == "information_only" {
			if err := makeInformationOnlyAppcast(filepath.Join(scenarioRoot, "feed", "appcast.xml")); err != nil {
				return err
			}
		}
	}

	sourceWorkspace := filepath.Join(scenarioRoot, "source-work")
	sourceBundle, err := assembleFixture(ctx, repository, scenarioRoot, sourceWorkspace, compiled.source, sourceVersion, sourceBuild, feedURL, publicKey)
	if err != nil {
		return err
	}
	installed := filepath.Join(scenarioRoot, "install", "Codex Pulse.app")
	saved := filepath.Join(scenarioRoot, "saved", "Codex Pulse.app")
	if err := command(ctx, scenarioRoot, nil, "ditto", sourceBundle, installed); err != nil {
		return err
	}
	if err := command(ctx, scenarioRoot, nil, "ditto", sourceBundle, saved); err != nil {
		return err
	}
	configuration := control{
		Version: controlVersion, Root: scenarioRoot, DataDirectory: filepath.Join(scenarioRoot, "data"),
		RuntimeDir: filepath.Join(scenarioRoot, "runtime"), Preferences: filepath.Join(scenarioRoot, "data", "preferences.json"),
		Evidence: filepath.Join(scenarioRoot, "evidence.jsonl"), Result: filepath.Join(scenarioRoot, "result.json"),
		Scenario: scenario, Action: "update", SourceVersion: sourceVersion, TargetVersion: targetVersion,
		MarkerSession: markerSession, ExpectedSchema: targetSchema,
	}
	controlPath := filepath.Join(scenarioRoot, "install", "upgrade-e2e-control.json")
	if err := writeControl(controlPath, configuration); err != nil {
		return err
	}

	logPath := filepath.Join(scenarioRoot, "application.log")
	process, err := launchFixture(installed, logPath)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, process.cleanup(configuration.Evidence)) }()
	first, err := awaitResult(configuration.Result, 150*time.Second)
	if err != nil {
		return err
	}
	if err := waitProcess(process, configuration.Evidence, 10*time.Second); err != nil {
		return err
	}
	if scenario != "migration_failure" {
		if err := waitPIDExit(first.PID, process.executable, 10*time.Second); err != nil {
			return err
		}
		return validateTerminalResult(scenario, first)
	}
	if first.Stage != "migration_safe_mode" || first.FaultCode != "apply_failed" || first.Application != targetVersion || !first.BackupPresent {
		return fmt.Errorf("unexpected migration safe-mode result: %#v", first)
	}
	if err := waitPIDExit(first.PID, process.executable, 10*time.Second); err != nil {
		return err
	}
	configuration.Action = "verify_rollback"
	if err := writeControl(controlPath, configuration); err != nil {
		return err
	}
	if err := os.Remove(configuration.Result); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(installed); err != nil {
		return err
	}
	if err := command(ctx, scenarioRoot, nil, "ditto", saved, installed); err != nil {
		return err
	}
	rollback, err := launchFixture(installed, filepath.Join(scenarioRoot, "application-rollback.log"))
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, rollback.cleanup(configuration.Evidence)) }()
	final, err := awaitResult(configuration.Result, 60*time.Second)
	if err != nil {
		return err
	}
	if err := waitProcess(rollback, configuration.Evidence, 10*time.Second); err != nil {
		return err
	}
	if final.Stage != "rollback_verified" || final.Application != sourceVersion || final.SchemaVersion != targetSchema-1 || !final.MarkerPresent {
		return fmt.Errorf("unexpected rollback result: %#v", final)
	}
	if err := waitPIDExit(final.PID, rollback.executable, 10*time.Second); err != nil {
		return err
	}
	return nil
}

func assembleFixture(ctx context.Context, repository, scenarioRoot, workspace, binary, version, build, feedURL, publicKey string) (string, error) {
	if err := os.MkdirAll(filepath.Join(workspace, "bin"), 0o700); err != nil {
		return "", err
	}
	icon := filepath.Join(repository, "bin/.packaging/icons.icns")
	tray := filepath.Join(repository, "bin/.packaging/tray")
	assemble := filepath.Join(repository, "build/darwin/assemble_bundle.sh")
	framework := filepath.Join(repository, ".cache/sparkle/2.9.4/framework/Sparkle.framework")
	if err := command(ctx, workspace, nil, assemble, binary, icon, tray, filepath.Join(repository, "build/darwin/Info.plist"), framework, "bin/Codex Pulse.app", version, build); err != nil {
		return "", err
	}
	plist := filepath.Join(workspace, "bin/Codex Pulse.app/Contents/Info.plist")
	for _, args := range [][]string{
		{"-replace", "CFBundleIdentifier", "-string", fixtureBundleIdentifier(scenarioRoot), plist},
		{"-insert", "SUFeedURL", "-string", feedURL, plist},
		{"-insert", "SUPublicEDKey", "-string", publicKey, plist},
		{"-insert", "LSUIElement", "-bool", "YES", plist},
		{"-insert", "NSAppTransportSecurity", "-json", `{"NSAllowsArbitraryLoads":true,"NSAllowsLocalNetworking":true}`, plist},
	} {
		if err := command(ctx, workspace, nil, "plutil", args...); err != nil {
			return "", err
		}
	}
	bundle := filepath.Join(workspace, "bin/Codex Pulse.app")
	if err := command(ctx, workspace, nil, "codesign", "--force", "--deep", "--sign", "-", "--timestamp=none", bundle); err != nil {
		return "", err
	}
	if err := command(ctx, workspace, nil, "codesign", "--verify", "--deep", "--strict", bundle); err != nil {
		return "", err
	}
	return bundle, nil
}

func fixtureBundleIdentifier(scenarioRoot string) string {
	digest := sha256.Sum256([]byte(scenarioRoot))
	return fmt.Sprintf("com.sisyphussq.codexpulse.upgradee2e.%x", digest[:8])
}

func generateAppcast(ctx context.Context, repository, scenarioRoot, prefix, seed string) error {
	tool := filepath.Join(repository, ".cache/sparkle/2.9.4/tools/generate_appcast")
	environment := append(os.Environ(), "HOME="+filepath.Join(scenarioRoot, "home"))
	command := exec.CommandContext(ctx, tool, "--ed-key-file", "-", "--download-url-prefix", prefix+"/", "--embed-release-notes", "--maximum-versions", "1", "-o", filepath.Join(scenarioRoot, "feed", "appcast.xml"), filepath.Join(scenarioRoot, "feed"))
	command.Dir = repository
	command.Env = environment
	command.Stdin = strings.NewReader(seed + "\n")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generate appcast: %w: %s", err, redactOutput(output, seed))
	}
	if strings.Contains(string(output), seed) {
		return errors.New("generate appcast leaked synthetic seed")
	}
	return nil
}

func writeControl(path string, value control) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func launchFixture(bundle, logPath string) (*fixtureProcess, error) {
	if err := os.WriteFile(logPath, nil, 0o600); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := os.Chmod(logPath, 0o600); err != nil {
		return nil, err
	}
	command := exec.Command("open", "-n", "-W", "-g", "-o", logPath, "--stderr", logPath, bundle)
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := trackFixtureHelper(command)

	executable, err := filepath.EvalSymlinks(filepath.Join(bundle, "Contents", "MacOS", "Codex Pulse"))
	if err != nil {
		return nil, errors.Join(err, process.cleanup(""))
	}
	process.executable = executable
	pid, err := awaitExecutablePID(executable, 10*time.Second)
	if err != nil {
		return nil, errors.Join(err, process.cleanup(""))
	}
	process.appPID = pid
	return process, nil
}

func trackFixtureHelper(command *exec.Cmd) *fixtureProcess {
	process := &fixtureProcess{helper: command.Process, helperDone: make(chan struct{})}
	go func() {
		process.helperErr = command.Wait()
		close(process.helperDone)
	}()
	return process
}

func awaitExecutablePID(executable string, timeout time.Duration) (int, error) {
	pattern := "^" + regexp.QuoteMeta(executable) + "$"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := exec.Command("pgrep", "-f", pattern).Output()
		if err == nil {
			for _, line := range strings.Fields(string(output)) {
				pid, parseErr := strconv.Atoi(line)
				if parseErr == nil && processMatches(pid, executable) {
					return pid, nil
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, errors.New("timed out waiting for identity-checked application pid")
}

func awaitResult(path string, timeout time.Duration) (result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var value result
			if err := json.Unmarshal(data, &value); err != nil {
				return result{}, err
			}
			return value, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return result{}, err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return result{}, errors.New("timed out waiting for application result")
}

func validateTerminalResult(scenario string, value result) error {
	switch scenario {
	case "success":
		if value.Stage != "target_verified" || value.Application != targetVersion || value.SchemaVersion != targetSchema || !value.MarkerPresent || !value.BackupPresent {
			return fmt.Errorf("unexpected success result: %#v", value)
		}
	case "bad_signature":
		if value.Stage != "expected_failure" || value.Application != sourceVersion || value.FaultCode != "invalid_signature" || !value.MarkerPresent {
			return fmt.Errorf("unexpected bad-signature result: %#v", value)
		}
	case "offline":
		if value.Stage != "expected_failure" || value.Application != sourceVersion || value.FaultCode != "check" || !value.MarkerPresent {
			return fmt.Errorf("unexpected offline result: %#v", value)
		}
	case "information_only":
		if value.Stage != "information_only_verified" || value.Application != sourceVersion || !value.MarkerPresent {
			return fmt.Errorf("unexpected information-only result: %#v", value)
		}
	}
	return nil
}

func makeInformationOnlyAppcast(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const opening = "<item>"
	const information = "<item><link>https://example.com/codex-pulse-update</link><sparkle:informationalUpdate>true</sparkle:informationalUpdate>"
	updated := strings.Replace(string(data), opening, information, 1)
	if updated == string(data) {
		return errors.New("generated appcast has no item to mark information-only")
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func closedLoopbackURL() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return "http://" + address, nil
}

func waitProcess(process *fixtureProcess, evidencePath string, timeout time.Duration) error {
	if process == nil || process.helper == nil || process.helperDone == nil {
		return errors.New("application helper is unavailable")
	}
	select {
	case <-process.helperDone:
		if process.helperErr != nil {
			return fmt.Errorf("application helper exited unsuccessfully: %w", process.helperErr)
		}
		return nil
	case <-time.After(timeout):
		return errors.Join(errors.New("application helper did not exit"), process.cleanup(evidencePath))
	}
}

func (process *fixtureProcess) cleanup(evidencePath string) error {
	if process == nil {
		return nil
	}
	var cleanupErr error
	if pid := latestEvidencePID(evidencePath); pid > 0 {
		cleanupErr = errors.Join(cleanupErr, terminatePID(pid, process.executable))
	}
	cleanupErr = errors.Join(cleanupErr, terminatePID(process.appPID, process.executable))
	cleanupErr = errors.Join(cleanupErr, terminateProcess(process.helper))
	if process.helperDone != nil {
		select {
		case <-process.helperDone:
		case <-time.After(5 * time.Second):
			cleanupErr = errors.Join(cleanupErr, errors.New("application helper remained alive after cleanup"))
		}
	}
	if processMatches(process.appPID, process.executable) {
		cleanupErr = errors.Join(cleanupErr, errors.New("identity-checked application remained alive after cleanup"))
	}
	return cleanupErr
}

func latestEvidencePID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		var value result
		if json.Unmarshal([]byte(lines[index]), &value) == nil && value.PID > 0 {
			return value.PID
		}
	}
	return 0
}

func terminatePID(pid int, executable string) error {
	if pid <= 0 || !processMatches(pid, executable) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitForProcessMismatch(pid, executable, time.Second) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if !waitForProcessMismatch(pid, executable, 2*time.Second) {
		return errors.New("identity-checked application resisted termination")
	}
	return nil
}

func waitForProcessMismatch(pid int, executable string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processMatches(pid, executable) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processMatches(pid, executable)
}

func processMatches(pid int, executable string) bool {
	if pid <= 0 || executable == "" {
		return false
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	return err == nil && strings.TrimSpace(string(output)) == executable
}

func terminateProcess(process *os.Process) error {
	if process != nil {
		if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

func waitPIDExit(pid int, executable string, timeout time.Duration) error {
	if pid <= 0 {
		return errors.New("invalid result pid")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processMatches(pid, executable) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = terminatePID(pid, executable)
	return errors.New("relaunched application did not exit")
}

func command(ctx context.Context, directory string, environment []string, name string, arguments ...string) error {
	process := exec.CommandContext(ctx, name, arguments...)
	process.Dir = directory
	if environment != nil {
		process.Env = environment
	}
	output, err := process.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func redactOutput(output []byte, secret string) string {
	return strings.ReplaceAll(strings.TrimSpace(string(output)), secret, "[REDACTED]")
}
