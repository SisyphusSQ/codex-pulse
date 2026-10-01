package config

import "testing"

func TestDisabledComponentsIgnoreUnusedConnectionSettings(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Host = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled components must not require connections: %v", err)
	}
}

func TestCORSRejectsWildcardOrigins(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"*", "https://*.example.com"} {
		cfg.Server.CORSOrigins = []string{origin}
		if err := cfg.Validate(); err == nil {
			t.Errorf("wildcard origin accepted: %s", origin)
		}
	}
}
