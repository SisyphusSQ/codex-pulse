// Command m11privacy performs a synthetic-only privacy audit. It never opens a
// configured Codex Home or user database and emits only aggregate counts.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/indexer"
	privacyaudit "github.com/SisyphusSQ/codex-pulse/internal/privacy"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	bodyCanary  = "M11_E4_SYNTHETIC_BODY_CANARY_7F2C"
	tokenCanary = "m11-e4-synthetic-token-9d31"
	pathCanary  = "/private/m11-e4-synthetic/project"
	markerFile  = "codex-pulse-m11-privacy-v1\n"
)

type summary struct {
	Version                 string `json:"version"`
	Result                  string `json:"result"`
	SyntheticOnly           bool   `json:"syntheticOnly"`
	SchemaTables            int    `json:"schemaTables"`
	DatabaseStrings         int64  `json:"databaseStrings"`
	DatabaseBlobs           int64  `json:"databaseBlobs"`
	GeneratedFiles          int    `json:"generatedFiles"`
	GeneratedFields         int    `json:"generatedFields"`
	FrontendFiles           int    `json:"frontendFiles"`
	DesignSources           int    `json:"designSources"`
	ScreenshotArtifacts     int    `json:"screenshotArtifacts"`
	WorkflowFiles           int    `json:"workflowFiles"`
	PackageFiles            int    `json:"packageFiles"`
	PackageSymlinks         int    `json:"packageSymlinks"`
	ParserCanaryAbsent      bool   `json:"parserCanaryAbsent"`
	BackupCanaryAbsent      bool   `json:"backupCanaryAbsent"`
	PublicPathRejected      bool   `json:"publicPathRejected"`
	StartupLogRedacted      bool   `json:"startupLogRedacted"`
	FrontendCacheMemoryOnly bool   `json:"frontendCacheMemoryOnly"`
	ActionsState            string `json:"actionsState"`
	CleanupPassed           bool   `json:"cleanupPassed"`
}

var tsField = regexp.MustCompile(`(?m)^\s+"?([A-Za-z_$][A-Za-z0-9_$]*)"?\??:\s`)
var userAbsolutePath = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:[a-z]:)?[\\/]+users[\\/]`)

func main() {
	repo := flag.String("repo", ".", "repository root")
	artifact := flag.String("artifact", "", "required packaged app root")
	flag.Parse()
	result, err := execute(context.Background(), *repo, *artifact)
	if err != nil {
		fmt.Fprintln(os.Stderr, stableFailure(err))
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "M11-PRIV-099: encode content-free summary")
		os.Exit(1)
	}
}

func execute(ctx context.Context, repo, artifact string) (result summary, returnErr error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return result, err
	}
	root = filepath.Clean(root)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return result, errors.New("invalid repository root")
	}
	if artifact == "" {
		return result, errors.New("packaged artifact is required")
	}
	result = summary{Version: privacyaudit.ContractVersion, SyntheticOnly: true, ActionsState: "actions_disabled_by_user_static_contract"}

	runRoot, err := os.MkdirTemp("", "codex-pulse-m11-privacy-")
	if err != nil {
		return result, err
	}
	if err := os.Chmod(runRoot, 0o700); err != nil {
		_ = os.RemoveAll(runRoot)
		return result, err
	}
	if err := os.WriteFile(filepath.Join(runRoot, ".owner"), []byte(markerFile), 0o600); err != nil {
		_ = os.RemoveAll(runRoot)
		return result, err
	}
	defer func() {
		cleanupErr := removeOwnedRoot(runRoot)
		result.CleanupPassed = cleanupErr == nil
		returnErr = errors.Join(returnErr, cleanupErr)
	}()

	databaseReport, err := dynamicCanary(ctx, runRoot)
	if err != nil {
		return result, err
	}
	result.SchemaTables = databaseReport.Tables
	result.DatabaseStrings = databaseReport.Strings
	result.DatabaseBlobs = databaseReport.Blobs
	result.ParserCanaryAbsent = true
	result.BackupCanaryAbsent = true
	if err := privacyaudit.InspectPublicValue(map[string]any{"project": pathCanary}); !privacyaudit.IsFinding(err, privacyaudit.FindingAbsolutePath) {
		return result, errors.New("public path was not rejected")
	}
	result.PublicPathRejected = true

	result.GeneratedFiles, result.GeneratedFields, err = auditGeneratedBindings(filepath.Join(root, "frontend", "bindings"))
	if err != nil {
		return result, err
	}
	result.FrontendFiles, err = auditFrontendCache(filepath.Join(root, "frontend", "src"))
	if err != nil {
		return result, err
	}
	result.FrontendCacheMemoryOnly = true
	if err := auditStartupLog(filepath.Join(root, "main.go")); err != nil {
		return result, err
	}
	result.StartupLogRedacted = true
	userHome, err := os.UserHomeDir()
	if err != nil {
		return result, err
	}
	result.DesignSources, err = auditDesignSource(
		filepath.Join(root, "docs", "design", "front", "codex-pulse-liquid-glass.pen"),
		root,
		userHome,
	)
	if err != nil {
		return result, err
	}
	result.ScreenshotArtifacts, err = auditScreenshotManifest(root, filepath.Join(root, "docs", "test", "m11-e4-screenshot-manifest.sha256"))
	if err != nil {
		return result, err
	}
	result.WorkflowFiles, _, err = auditTree(filepath.Join(root, ".github", "workflows"), yamlArtifact)
	if err != nil {
		return result, err
	}
	artifactRoot := artifact
	if !filepath.IsAbs(artifactRoot) {
		artifactRoot = filepath.Join(root, artifactRoot)
	}
	result.PackageFiles, result.PackageSymlinks, err = auditTree(filepath.Clean(artifactRoot), allRegularArtifact, root, userHome)
	if err != nil {
		return result, err
	}
	if result.SchemaTables == 0 || result.DatabaseStrings == 0 || result.DatabaseBlobs == 0 ||
		result.GeneratedFiles == 0 || result.GeneratedFields == 0 ||
		result.FrontendFiles == 0 || result.DesignSources == 0 || result.ScreenshotArtifacts == 0 ||
		result.WorkflowFiles == 0 || result.PackageFiles == 0 {
		return result, errors.New("required privacy audit surface is empty")
	}
	if result.PackageSymlinks == 0 {
		return result, errors.New("packaged artifact symlink surface is empty")
	}
	result.Result = "passed"
	return result, nil
}

func dynamicCanary(ctx context.Context, root string) (privacyaudit.DatabaseReport, error) {
	home := filepath.Join(root, "home")
	sessions := filepath.Join(home, "sessions", "2026", "07")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	rollout := strings.Join([]string{
		`{"timestamp":"2026-07-19T00:00:00Z","type":"session_meta","payload":{"id":"session-privacy","timestamp":"2026-07-19T00:00:00Z","cwd":"` + pathCanary + `","originator":"codex_cli_rs","cli_version":"0.1.0","source":"cli","model_provider":"openai"}}`,
		`{"timestamp":"2026-07-19T00:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-privacy","started_at":1784419201}}`,
		`{"timestamp":"2026-07-19T00:00:02Z","type":"turn_context","payload":{"turn_id":"turn-privacy","cwd":"` + pathCanary + `","model":"gpt-5.2-codex","effort":"high"}}`,
		`{"timestamp":"2026-07-19T00:00:03Z","type":"response_item","payload":{"type":"message","content":[{"type":"output_text","text":"` + bodyCanary + `"}],"authorization":"Bearer ` + tokenCanary + `"}}`,
		`{"timestamp":"2026-07-19T00:00:04Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-privacy","completed_at":1784419204,"last_agent_message":"` + bodyCanary + `"}}`,
	}, "\n") + "\n"
	rolloutPath := filepath.Join(sessions, "rollout-privacy.jsonl")
	if err := os.WriteFile(rolloutPath, []byte(rollout), 0o600); err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(root, "store", "codex-pulse.db")})
	if err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	defer database.Close(context.Background())
	repository := store.NewRepository(database)
	if _, err := repository.MigrateApplicationSchema(ctx); err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	discovery, err := discoverer.Discover(ctx)
	if err != nil || len(discovery.Issues) != 0 {
		return privacyaudit.DatabaseReport{}, errors.New("synthetic discovery failed")
	}
	plan, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		return privacyaudit.DatabaseReport{}, errors.New("synthetic reconcile failed")
	}
	ingester, err := indexer.New(repository)
	if err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	stream, err := ingester.Open(ctx, indexer.OpenRequest{Action: plan.Actions[0], AtMS: time.Now().UnixMilli()})
	if err != nil {
		return privacyaudit.DatabaseReport{}, err
	}
	feed, err := stream.Feed(ctx, []byte(rollout), true, time.Now().UnixMilli())
	if err != nil || !feed.Committed {
		return privacyaudit.DatabaseReport{}, errors.New("synthetic ingest failed")
	}
	if encoded, err := json.Marshal(feed); err != nil || privacyaudit.InspectArtifact(encoded, bodyCanary, tokenCanary) != nil {
		return privacyaudit.DatabaseReport{}, errors.New("parser result leaked canary")
	}
	report, err := privacyaudit.InspectDatabase(ctx, database)
	if err != nil {
		return report, err
	}
	backupPath := filepath.Join(root, "backups", "privacy.db")
	if _, err := database.Backup(ctx, storesqlite.BackupOptions{Destination: backupPath}); err != nil {
		return report, err
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		return report, err
	}
	if err := privacyaudit.InspectArtifact(backup, bodyCanary, tokenCanary); err != nil {
		return report, errors.New("backup leaked canary")
	}
	return report, nil
}

func auditGeneratedBindings(root string) (files, fields int, returnErr error) {
	returnErr = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".ts" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for _, match := range tsField.FindAllSubmatch(content, -1) {
			fields++
			if err := privacyaudit.InspectField(string(match[1])); err != nil {
				return errors.New("generated binding contains forbidden field")
			}
		}
		return nil
	})
	return
}

func auditFrontendCache(root string) (files int, returnErr error) {
	forbidden := [][]byte{[]byte("localStorage"), []byte("sessionStorage"), []byte("indexedDB"), []byte("persistQueryClient")}
	returnErr = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.Contains(path, ".test.") {
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".ts" && extension != ".vue" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for _, marker := range forbidden {
			if bytes.Contains(content, marker) {
				return errors.New("frontend cache persistence is not allowed")
			}
		}
		return nil
	})
	return
}

func auditDesignSource(path string, canaries ...string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var decoded any
	if json.Unmarshal(content, &decoded) != nil {
		return 0, errors.New("design source is invalid")
	}
	markers := append([]string{bodyCanary, tokenCanary}, canaries...)
	if designValueContainsPrivateArtifact(decoded, markers...) {
		return 0, errors.New("design source contains private artifact value")
	}
	return 1, nil
}

func designValueContainsPrivateArtifact(value any, canaries ...string) bool {
	switch typed := value.(type) {
	case string:
		return userAbsolutePath.MatchString(typed) || privacyaudit.InspectArtifact([]byte(typed), canaries...) != nil
	case []any:
		for _, item := range typed {
			if designValueContainsPrivateArtifact(item, canaries...) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if designValueContainsPrivateArtifact(key, canaries...) ||
				designValueContainsPrivateArtifact(item, canaries...) {
				return true
			}
		}
	}
	return false
}

func auditStartupLog(path string) error {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return err
	}
	foundStable := false
	var unsafe bool
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isOutputCall(call.Fun) {
			if isSafeStartupCall(call) {
				foundStable = true
			} else {
				unsafe = true
			}
		}
		return true
	})
	if unsafe {
		return errors.New("startup log can expose raw error")
	}
	if !foundStable {
		return errors.New("startup log has no stable classification")
	}
	return nil
}

func isOutputCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch identifier.Name {
	case "log":
		return strings.HasPrefix(selector.Sel.Name, "Print") || strings.HasPrefix(selector.Sel.Name, "Fatal") || strings.HasPrefix(selector.Sel.Name, "Panic")
	case "slog":
		return selector.Sel.Name == "Debug" || selector.Sel.Name == "Info" || selector.Sel.Name == "Warn" || selector.Sel.Name == "Error" || selector.Sel.Name == "ErrorContext" || selector.Sel.Name == "Log"
	case "fmt":
		return strings.HasPrefix(selector.Sel.Name, "Print") || strings.HasPrefix(selector.Sel.Name, "Fprint")
	default:
		return false
	}
}

func isSafeStartupCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Print" || len(call.Args) != 1 {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || identifier.Name != "log" {
		return false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	return ok && literal.Kind == token.STRING && literal.Value == `"codex-pulse startup_failed"`
}

type artifactPredicate func(string) bool

func yamlArtifact(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".yml" || extension == ".yaml"
}
func allRegularArtifact(string) bool { return true }

func auditTree(root string, include artifactPredicate, canaries ...string) (files, symlinks int, returnErr error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return 0, 0, errors.New("invalid artifact root")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return 0, 0, errors.New("invalid artifact root")
	}
	returnErr = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil || !pathWithin(resolvedRoot, target) {
				return errors.New("artifact symlink escapes root")
			}
			symlinks++
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() || !include(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		if err := privacyaudit.InspectArtifact(content, append([]string{bodyCanary, tokenCanary}, canaries...)...); err != nil {
			return errors.New("artifact contains synthetic privacy canary")
		}
		return nil
	})
	return
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func auditScreenshotManifest(root, manifestPath string) (int, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	count := 0
	manifestPaths := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "  ", 2)
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 {
			return 0, errors.New("invalid screenshot manifest")
		}
		manifestRelative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(parts[1])))
		if manifestRelative != parts[1] || !strings.HasPrefix(manifestRelative, "docs/") {
			return 0, errors.New("invalid screenshot manifest path")
		}
		if _, duplicate := manifestPaths[manifestRelative]; duplicate {
			return 0, errors.New("duplicate screenshot manifest path")
		}
		manifestPaths[manifestRelative] = struct{}{}
		path := filepath.Join(root, filepath.FromSlash(manifestRelative))
		if !pathWithin(root, path) {
			return 0, errors.New("screenshot manifest escapes root")
		}
		content, err := os.ReadFile(path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(content)) != parts[0] {
			return 0, errors.New("screenshot manifest mismatch")
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	actualPaths := make(map[string]struct{})
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		extension := strings.ToLower(filepath.Ext(path))
		if entry.Type().IsRegular() && (extension == ".png" || extension == ".jpg" || extension == ".jpeg") {
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr != nil {
				return relativeErr
			}
			actualPaths[filepath.ToSlash(relative)] = struct{}{}
		}
		return nil
	})
	if err != nil || count == 0 || len(actualPaths) != count {
		return 0, errors.New("screenshot manifest is incomplete")
	}
	for path := range actualPaths {
		if _, exists := manifestPaths[path]; !exists {
			return 0, errors.New("screenshot manifest path set mismatch")
		}
	}
	return count, nil
}

func removeOwnedRoot(root string) error {
	marker, err := os.ReadFile(filepath.Join(root, ".owner"))
	if err != nil || string(marker) != markerFile || !strings.HasPrefix(filepath.Base(root), "codex-pulse-m11-privacy-") {
		return errors.New("refusing unowned cleanup")
	}
	return os.RemoveAll(root)
}

func stableFailure(err error) string {
	switch {
	case privacyaudit.IsFinding(err, privacyaudit.FindingForbiddenField):
		return "M11-PRIV-010: forbidden field contract failed"
	case privacyaudit.IsFinding(err, privacyaudit.FindingSensitiveValue):
		return "M11-PRIV-011: sensitive value contract failed"
	case privacyaudit.IsFinding(err, privacyaudit.FindingAbsolutePath):
		return "M11-PRIV-012: public path contract failed"
	default:
		return "M11-PRIV-019: privacy audit failed"
	}
}
