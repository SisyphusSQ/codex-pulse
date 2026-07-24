package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRuntimeConfigRequiresSocketAndInheritedFD(t *testing.T) {
	explicitHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", explicitHome)
	config, err := parseRuntimeConfig([]string{
		"--socket", "/private/tmp/cp/core.sock",
		"--auth-fd", "3",
		"--database-path", "/private/tmp/cp/data/codex-pulse.db",
		"--preferences-path", "/private/tmp/cp/preferences.json",
	})
	if err != nil {
		t.Fatalf("parseRuntimeConfig() error = %v", err)
	}
	if config.SocketPath != "/private/tmp/cp/core.sock" || config.AuthFD != 3 || config.HelperVersion == "" ||
		config.DatabasePath != "/private/tmp/cp/data/codex-pulse.db" ||
		config.PreferencesPath != "/private/tmp/cp/preferences.json" ||
		config.DefaultCodexHome != explicitHome {
		t.Fatalf("config = %#v", config)
	}
	defaultConfig, err := parseRuntimeConfig([]string{"--socket", "/private/tmp/cp/core.sock", "--auth-fd", "3"})
	if err != nil {
		t.Fatalf("parseRuntimeConfig(default paths) error = %v", err)
	}
	if defaultConfig.DatabasePath != "" || defaultConfig.PreferencesPath != "" {
		t.Fatalf("default path overrides = %#v, want empty", defaultConfig)
	}
	for _, arguments := range [][]string{
		nil,
		{"--socket", "/private/tmp/cp/core.sock"},
		{"--auth-fd", "3"},
		{"--socket", "/private/tmp/cp/core.sock", "--auth-fd", "2"},
		{"--socket", "/private/tmp/cp/core.sock", "--auth-fd", "3", "extra"},
	} {
		if _, err := parseRuntimeConfig(arguments); err == nil {
			t.Fatalf("parseRuntimeConfig(%q) succeeded", arguments)
		}
	}
}

func TestParseRuntimeConfigDefaultsCodexHomeToUserHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	config, err := parseRuntimeConfig([]string{
		"--socket", "/private/tmp/cp/core.sock",
		"--auth-fd", "3",
	})
	if err != nil {
		t.Fatalf("parseRuntimeConfig() error = %v", err)
	}
	want := filepath.Join(userHome, ".codex")
	if config.DefaultCodexHome != want {
		t.Fatalf("DefaultCodexHome = %q, want %q", config.DefaultCodexHome, want)
	}
}
