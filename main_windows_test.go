//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

func TestWindowsRuntimeRequiresPrivatePipeArguments(t *testing.T) {
	runtime := t.TempDir()
	home := filepath.Join(runtime, "home")
	t.Setenv("CODEX_HOME", home)
	config, err := parseRuntimeConfig([]string{"--pipe", `\\.\pipe\codex-pulse-test`, "--auth-fd", "123", "--runtime-directory", runtime})
	if err != nil || config.DefaultCodexHome != home || config.DatabasePath != filepath.Join(runtime, "codex-pulse.db") || config.PreferencesPath != filepath.Join(runtime, "preferences.json") {
		t.Fatalf("configuration = %#v, %v", config, err)
	}
	for _, args := range [][]string{
		{"--socket", `C:\runtime\core.sock`, "--auth-fd", "123"},
		{"--pipe", `\\.\pipe\codex-pulse-test`, "--auth-fd", "123"},
		{"--pipe", `\\.\pipe\codex-pulse-test`, "--auth-fd", "2", "--runtime-directory", runtime},
	} {
		if _, err := parseRuntimeConfig(args); err == nil {
			t.Fatalf("accepted invalid arguments: %q", args)
		}
	}
}
