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

func TestHTTPRequiresExplicitPrivateBinding(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "0.0.0.0:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("public HTTP binding accepted")
	}
	cfg.Server.Address = "100.100.100.100:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Server.AllowHTTP = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("HTTP origin accepted without explicit mode")
	}
}

func TestDeploymentExamplesValidateWithoutConnecting(t *testing.T) {
	for _, file := range []string{"../deploy/https-sqlite.yml", "../deploy/private-http-sqlite.yml", "../deploy/https-mysql.yml", "config_docker.yml", "development.example.yml", "production.example.yml"} {
		if _, err := Load(file); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}
