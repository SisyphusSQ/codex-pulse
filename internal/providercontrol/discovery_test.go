package providercontrol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestFirstAvailablePrefersAccessiblePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "projects")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	got := firstAvailable(
		probeDirectory(filepath.Join(root, "missing")),
		probeDirectory(dir),
	)
	if got.State != DiscoveryAvailable {
		t.Fatalf("firstAvailable() = %#v, want available", got)
	}
}

func TestWorseProbeKeepsInvalidOverMissing(t *testing.T) {
	t.Parallel()
	got := worseProbe(
		ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound},
		ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonUnsafePath},
	)
	if got.State != DiscoveryInvalid || got.ReasonCode != ReasonUnsafePath {
		t.Fatalf("worseProbe() = %#v", got)
	}
}

func TestSafeAbsolutePathAllowsDotsInsidePathComponents(t *testing.T) {
	t.Parallel()
	if !safeAbsolutePath(filepath.Join(t.TempDir(), "client..archive")) {
		t.Fatal("safeAbsolutePath() rejected a normalized absolute path")
	}
}

func TestDefaultProbesAreMetadataOnlyAndOmitSecrets(t *testing.T) {
	root := t.TempDir()
	cursorHome := filepath.Join(root, "cursor-home")
	grokHome := filepath.Join(root, "grok-home")
	if err := os.MkdirAll(filepath.Join(cursorHome, ".cursor", "projects"), 0o700); err != nil {
		t.Fatalf("mkdir cursor projects: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(grokHome, "sessions"), 0o700); err != nil {
		t.Fatalf("mkdir grok sessions: %v", err)
	}
	secret := "super-secret-token-value"
	if err := os.WriteFile(filepath.Join(grokHome, "auth.json"), []byte(`{"token":"`+secret+`"}`), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	t.Setenv("CODEX_PULSE_CURSOR_HOME", cursorHome)
	t.Setenv("CODEX_PULSE_GROK_HOME", grokHome)
	probes := DefaultProbes(nil, func() string { return filepath.Join(root, "missing-codex") })
	cursor := probes.Cursor(context.Background())
	grok := probes.Grok(context.Background())
	codex := probes.Codex(context.Background(), nil)
	if cursor.State != DiscoveryAvailable || grok.State != DiscoveryAvailable ||
		codex.State != DiscoveryMissing {
		t.Fatalf("probes cursor=%#v grok=%#v codex=%#v", cursor, grok, codex)
	}
	for _, result := range []ProbeResult{cursor, grok, codex} {
		if strings.Contains(result.ReasonCode, secret) || strings.Contains(result.ReasonCode, root) {
			t.Fatalf("probe leaked context: %#v", result)
		}
	}
}

func TestConfiguredCodexProbeRequiresConfirmedPhysicalIdentity(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	metadata, err := logsource.NewHomeProbe().ProbeIdentity(context.Background(), home)
	if err != nil {
		t.Fatalf("ProbeIdentity() error = %v", err)
	}
	probes := DefaultProbes(nil, nil)
	configured := &preferences.CodexHomePreferences{Source: preferences.ConfirmedSource{
		Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
	}}
	if got := probes.Codex(context.Background(), configured); got.State != DiscoveryAvailable {
		t.Fatalf("Codex(valid identity) = %#v", got)
	}
	configured.Source.Inode++
	if got := probes.Codex(context.Background(), configured); got.State != DiscoveryInvalid || got.ReasonCode != ReasonUnsafePath {
		t.Fatalf("Codex(replaced identity) = %#v", got)
	}
}
