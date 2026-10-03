package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

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

func TestMigrationTimeoutDefaultAndValidation(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.MigrationTimeout != 2*time.Minute {
		t.Fatal("migration default", cfg.Database.MigrationTimeout)
	}
	t.Setenv("APP_DATABASE_MIGRATIONTIMEOUT", "30s")
	cfg, err = Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.MigrationTimeout != 30*time.Second {
		t.Fatal("migration environment override")
	}
	cfg.Database.Enabled = true
	cfg.Database.MigrationTimeout = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("unbounded migration accepted")
	}
}

func TestCORSAllowsOnlyStandaloneWildcardOrExactOrigins(t *testing.T) {
	cfg, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, origins := range [][]string{{"*", "https://example.com"}, {"https://*.example.com"}, {"https://example.com/path"}} {
		cfg.Server.CORSOrigins = origins
		if err := cfg.Validate(); err == nil {
			t.Errorf("invalid CORS origins accepted: %v", origins)
		}
	}
}

func TestCORSDefaultAndExplicitEmpty(t *testing.T) {
	content, err := os.ReadFile("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, cors string
		want       []string
	}{
		{"omitted", "", []string{"*"}},
		{"wildcard", `    corsOrigins: ["*"]`, []string{"*"}},
		{"empty", "    corsOrigins: []", []string{}},
		{"exact", `    corsOrigins: ["https://example.com"]`, []string{"https://example.com"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var lines []string
			for _, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, "corsOrigins:") {
					line = tt.cors
				}
				lines = append(lines, line)
			}
			file := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(file)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Server.CORSOrigins, tt.want) {
				t.Fatalf("CORS origins: %v", cfg.Server.CORSOrigins)
			}
		})
	}
}
