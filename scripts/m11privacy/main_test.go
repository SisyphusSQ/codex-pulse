package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteAuditsSyntheticPipelineAndCleansIsolation(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	artifact := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifact, "safe.bin"), []byte("safe packaged fixture"), 0o600); err != nil {
		t.Fatalf("write artifact fixture: %v", err)
	}
	if err := os.Symlink("safe.bin", filepath.Join(artifact, "safe-link")); err != nil {
		t.Fatalf("write artifact symlink: %v", err)
	}
	result, err := execute(context.Background(), root, artifact)
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if result.Result != "passed" || !result.SyntheticOnly || result.SchemaTables == 0 ||
		result.GeneratedFiles == 0 || result.GeneratedFields == 0 || result.FrontendFiles == 0 || result.ScreenshotArtifacts == 0 ||
		result.DesignSources == 0 ||
		result.WorkflowFiles == 0 || result.PackageFiles == 0 || result.PackageSymlinks == 0 ||
		!result.ParserCanaryAbsent || !result.BackupCanaryAbsent || !result.PublicPathRejected ||
		!result.StartupLogRedacted || !result.FrontendCacheMemoryOnly || !result.CleanupPassed {
		t.Fatalf("execute() = %#v", result)
	}
}

func TestAuditDesignSourceRejectsAbsoluteUserPath(t *testing.T) {
	for name, value := range map[string]string{
		"macos":            "/Users/example/private-project",
		"windows_slash":    "C:/Users/example/private-project",
		"windows_escape":   `C:\Users\example\private-project`,
		"windows_casefold": `d:/uSeRs/example/private-project`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "design.pen")
			content, err := json.Marshal(map[string]any{"children": []any{map[string]any{"content": value}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := auditDesignSource(path); err == nil {
				t.Fatalf("auditDesignSource() accepted absolute user path %q", value)
			}
		})
	}
}

func TestAuditDesignSourceRejectsPrivateObjectKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "design.pen")
	content, err := json.Marshal(map[string]any{`C:\Users\example\private-project`: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auditDesignSource(path); err == nil {
		t.Fatal("auditDesignSource() accepted private object key")
	}
}

func TestAuditStartupLogRejectsRawError(t *testing.T) {
	for name, source := range map[string]string{
		"log":       `package main; import "log"; func main() { log.Printf("startup_failed: %s", err) }`,
		"log_alias": `package main; import "log"; func main() { cause := err; log.Print(cause) }`,
		"slog":      `package main; import "log/slog"; func main() { slog.Error("startup_failed", "error", err) }`,
		"fmt":       `package main; import ("fmt"; "os"); func main() { fmt.Fprintln(os.Stderr, "startup_failed", err) }`,
		"fmt_print": `package main; import "fmt"; func main() { fmt.Printf("startup_failed: %v", err) }`,
	} {
		path := filepath.Join(t.TempDir(), name+".go")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := auditStartupLog(path); err == nil {
			t.Fatalf("auditStartupLog() accepted %s raw error logging", name)
		}
	}
}

func TestAuditScreenshotManifestRejectsDuplicatePathSet(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "docs", "images")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	first := []byte("first image fixture")
	second := []byte("second image fixture")
	if err := os.WriteFile(filepath.Join(directory, "first.png"), first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "second.png"), second, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "manifest.sha256")
	duplicate := fmt.Sprintf("%x  docs/images/first.png\n%x  docs/images/first.png\n", sha256.Sum256(first), sha256.Sum256(first))
	if err := os.WriteFile(manifest, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auditScreenshotManifest(root, manifest); err == nil {
		t.Fatal("auditScreenshotManifest() accepted duplicate path and omitted image")
	}
}

func TestExecuteRequiresPackagedArtifact(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execute(context.Background(), root, ""); err == nil {
		t.Fatal("execute() accepted missing packaged artifact")
	}
}

func TestStaticAuditorsRejectForbiddenBindingCacheAndArtifact(t *testing.T) {
	root := t.TempDir()
	bindings := filepath.Join(root, "bindings")
	if err := os.MkdirAll(bindings, 0o700); err != nil {
		t.Fatalf("create bindings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bindings, "models.ts"), []byte("export interface Leak {\n  \"accessToken\": string;\n}\n"), 0o600); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	if _, _, err := auditGeneratedBindings(bindings); err == nil {
		t.Fatal("auditGeneratedBindings() accepted accessToken")
	}

	frontend := filepath.Join(root, "frontend")
	if err := os.MkdirAll(frontend, 0o700); err != nil {
		t.Fatalf("create frontend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(frontend, "cache.ts"), []byte("localStorage.setItem('query', 'value')"), 0o600); err != nil {
		t.Fatalf("write frontend: %v", err)
	}
	if _, err := auditFrontendCache(frontend); err == nil {
		t.Fatal("auditFrontendCache() accepted Web Storage")
	}

	artifacts := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		t.Fatalf("create artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "leak.bin"), []byte(bodyCanary), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, _, err := auditTree(artifacts, allRegularArtifact); err == nil {
		t.Fatal("auditTree() accepted body canary")
	}
}

func TestAuditTreeRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auditTree(root, allRegularArtifact); err == nil {
		t.Fatal("auditTree() accepted escaping symlink")
	}
}

func TestStableFailureNeverEchoesCause(t *testing.T) {
	if got := stableFailure(os.ErrPermission); got != "M11-PRIV-019: privacy audit failed" {
		t.Fatalf("stableFailure() = %q", got)
	}
}
