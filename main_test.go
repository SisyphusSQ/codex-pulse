package main

import "testing"

func TestParseRuntimeConfigRequiresSocketAndInheritedFD(t *testing.T) {
	config, err := parseRuntimeConfig([]string{"--socket", "/private/tmp/cp/core.sock", "--auth-fd", "3"})
	if err != nil {
		t.Fatalf("parseRuntimeConfig() error = %v", err)
	}
	if config.SocketPath != "/private/tmp/cp/core.sock" || config.AuthFD != 3 || config.HelperVersion == "" {
		t.Fatalf("config = %#v", config)
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
