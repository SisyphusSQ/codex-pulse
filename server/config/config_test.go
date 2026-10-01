package config

import "testing"

func TestLoadDefaultConfig(t *testing.T) {
	t.Setenv("APP_SERVER_ADDRESS", "127.0.0.1:9090")
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Address != "127.0.0.1:9090" {
		t.Fatalf("Server.Address = %q, want 127.0.0.1:9090", cfg.Server.Address)
	}
	if cfg.ContextTimeout <= 0 {
		t.Fatalf("ContextTimeout = %s, want positive duration", cfg.ContextTimeout)
	}
}

func TestValidateRejectsIncompleteJWT(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Key.Type = "jwt"
	cfg.Key.JWT.Secret = "short"
	if err = cfg.Validate(); err == nil {
		t.Fatal("Validate() expected JWT error, got nil")
	}
}

func TestValidateRejectsUnauthenticatedNonDebugMode(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Debug = false
	cfg.Key.Type = "none"
	if err = cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted key.type none outside debug mode")
	}
}
