// probe-binding checks only the explicitly requested Home/runtime of an owned App
// and Helper. It never prints full environments, paths, command lines or credentials.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/process"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "binding_probe_failed")
		os.Exit(1)
	}
}
func run() error {
	flags := flag.NewFlagSet("probe-binding", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	appPID := flags.Int("app-pid", 0, "")
	helperPID := flags.Int("helper-pid", 0, "")
	home := flags.String("home", "", "")
	runtime := flags.String("runtime-directory", "", "")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *appPID <= 0 || *helperPID <= 0 || !filepath.IsAbs(*home) || !filepath.IsAbs(*runtime) {
		return errors.New("invalid probe configuration")
	}
	ctx := context.Background()
	app, err := process.NewProcess(int32(*appPID))
	if err != nil {
		return err
	}
	helper, err := process.NewProcess(int32(*helperPID))
	if err != nil {
		return err
	}
	parent, err := helper.PpidWithContext(ctx)
	if err != nil || parent != int32(*appPID) {
		return errors.New("unexpected helper parent")
	}
	appHome, err := processHome(ctx, app)
	if err != nil {
		return err
	}
	helperHome, err := processHome(ctx, helper)
	if err != nil {
		return err
	}
	expected, err := logsource.NewHomeProbe().Probe(ctx, *home)
	if err != nil {
		return err
	}
	actualApp, err := logsource.NewHomeProbe().Probe(ctx, appHome)
	if err != nil {
		return err
	}
	actualHelper, err := logsource.NewHomeProbe().Probe(ctx, helperHome)
	if err != nil {
		return err
	}
	info, err := os.Stat(*runtime)
	if err != nil || !platformfs.PrivateMode(*runtime, info, 0o700) {
		return errors.New("runtime not private")
	}
	store, err := preferences.NewFileStore(filepath.Join(*runtime, "preferences.json"))
	if err != nil {
		return err
	}
	snapshot, err := store.LoadPreferences(ctx)
	if err != nil || snapshot.CodexHome == nil {
		return errors.New("Home not confirmed")
	}
	args, err := helper.CmdlineSliceWithContext(ctx)
	if err != nil {
		return err
	}
	runtimeMatches := false
	for index, arg := range args {
		if arg == "--runtime-directory" && index+1 < len(args) {
			runtimeMatches = strings.EqualFold(filepath.Clean(args[index+1]), filepath.Clean(*runtime))
		}
	}
	same := func(path logsource.HomeMetadata) bool {
		return path.DeviceID == expected.DeviceID && path.Inode == expected.Inode
	}
	matched := same(actualApp) && same(actualHelper) && snapshot.CodexHome.Source.DeviceID == expected.DeviceID && snapshot.CodexHome.Source.Inode == expected.Inode
	if !matched || !runtimeMatches {
		return errors.New("binding identity mismatch")
	}
	hash := sha256.Sum256([]byte(expected.DeviceID))
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"app_pid": *appPID, "helper_pid": *helperPID, "app_home_matches": same(actualApp), "helper_home_matches": same(actualHelper), "preferences_home_matches": matched, "helper_runtime_matches": runtimeMatches, "private_runtime": true, "home_identity_hash": hex.EncodeToString(hash[:8])})
}
func processHome(ctx context.Context, p *process.Process) (string, error) {
	environment, err := p.EnvironWithContext(ctx)
	if err != nil {
		return "", err
	}
	value := ""
	for index, entry := range environment {
		key, content, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "CODEX_HOME") {
			value = content
		}
		environment[index] = ""
	}
	if value == "" {
		return "", errors.New("CODEX_HOME missing")
	}
	return value, nil
}
