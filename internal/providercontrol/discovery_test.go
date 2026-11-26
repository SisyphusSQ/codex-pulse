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

func TestAGYProbeClassifiesMissingInaccessibleUnsafeAndAvailable(t *testing.T) {
	probes := DefaultProbes(nil, nil)

	// 1. 缺失根目录 -> not_found
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("CODEX_PULSE_AGY_HOME", missingRoot)
	t.Setenv("ANTIGRAVITY_GEMINI_DIR", "")
	if got := probes.AGY(context.Background()); got.State != DiscoveryMissing || got.ReasonCode != ReasonNotFound {
		t.Fatalf("AGY(missing root) = %#v, want missing/not_found", got)
	}

	// 2. 空产品目录 -> not_found
	emptyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(emptyRoot, "antigravity"), 0o700); err != nil {
		t.Fatalf("mkdir empty product: %v", err)
	}
	t.Setenv("CODEX_PULSE_AGY_HOME", emptyRoot)
	if got := probes.AGY(context.Background()); got.State != DiscoveryMissing || got.ReasonCode != ReasonNotFound {
		t.Fatalf("AGY(empty product dir) = %#v, want missing/not_found", got)
	}

	// 3. 相对路径 / 非规范路径 -> unsafe_path
	t.Setenv("CODEX_PULSE_AGY_HOME", "relative/gemini")
	if got := probes.AGY(context.Background()); got.State != DiscoveryInvalid || got.ReasonCode != ReasonUnsafePath {
		t.Fatalf("AGY(relative env) = %#v, want invalid/unsafe_path", got)
	}

	// 4. 符号链接产品目录 -> unsafe_path
	symlinkRoot := t.TempDir()
	realTarget := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realTarget, "conversations"), 0o700); err != nil {
		t.Fatalf("mkdir real target: %v", err)
	}
	if err := os.Symlink(realTarget, filepath.Join(symlinkRoot, "antigravity")); err != nil {
		t.Fatalf("symlink product: %v", err)
	}
	t.Setenv("CODEX_PULSE_AGY_HOME", symlinkRoot)
	if got := probes.AGY(context.Background()); got.State != DiscoveryInvalid || got.ReasonCode != ReasonUnsafePath {
		t.Fatalf("AGY(symlink product) = %#v, want invalid/unsafe_path", got)
	}

	// 5. 无权限产品目录 -> permission_denied（非 root 用户）
	if os.Geteuid() != 0 {
		noPermRoot := t.TempDir()
		noPermProduct := filepath.Join(noPermRoot, "antigravity-ide")
		if err := os.MkdirAll(noPermProduct, 0o700); err != nil {
			t.Fatalf("mkdir noPermProduct: %v", err)
		}
		if err := os.Chmod(noPermProduct, 0o000); err != nil {
			t.Fatalf("chmod 0000: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(noPermProduct, 0o700) })
		t.Setenv("CODEX_PULSE_AGY_HOME", noPermRoot)
		if got := probes.AGY(context.Background()); got.State != DiscoveryInaccessible || got.ReasonCode != ReasonPermissionDenied {
			t.Fatalf("AGY(inaccessible) = %#v, want inaccessible/permission_denied", got)
		}
	}

	// 6. 可用：任一产品目录含关键文件/目录，且严格 metadata-only 不读 SQLite 正文
	availRoot := t.TempDir()
	cliProduct := filepath.Join(availRoot, "antigravity-cli")
	if err := os.MkdirAll(cliProduct, 0o700); err != nil {
		t.Fatalf("mkdir cliProduct: %v", err)
	}
	secret := "agy-secret-prompt-body-never-read"
	if err := os.WriteFile(filepath.Join(cliProduct, "conversation_summaries.db"), []byte("not-a-real-sqlite:"+secret), 0o600); err != nil {
		t.Fatalf("write summaries db: %v", err)
	}
	t.Setenv("CODEX_PULSE_AGY_HOME", availRoot)
	t.Setenv("ANTIGRAVITY_GEMINI_DIR", missingRoot)
	got := probes.AGY(context.Background())
	if got.State != DiscoveryAvailable || got.ReasonCode != ReasonAvailable {
		t.Fatalf("AGY(available) = %#v, want available/available", got)
	}
	if strings.Contains(got.ReasonCode, secret) || strings.Contains(got.ReasonCode, availRoot) {
		t.Fatalf("AGY probe leaked content or path: %#v", got)
	}

	// 7. ANTIGRAVITY_GEMINI_DIR 回退生效（brain/ 目录可用）
	fallbackRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fallbackRoot, "antigravity-idea", "brain"), 0o700); err != nil {
		t.Fatalf("mkdir idea brain: %v", err)
	}
	t.Setenv("CODEX_PULSE_AGY_HOME", "")
	t.Setenv("ANTIGRAVITY_GEMINI_DIR", fallbackRoot)
	if got := probes.AGY(context.Background()); got.State != DiscoveryAvailable || got.ReasonCode != ReasonAvailable {
		t.Fatalf("AGY(ANTIGRAVITY_GEMINI_DIR) = %#v, want available", got)
	}
}
