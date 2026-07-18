package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

const (
	confirmationPhrase = "READ_ONLY_CONFIRMED"
	contractVersion    = "m11-real-home-v1"
	isolationMarker    = "codex-pulse-m11-real-home-v1\n"
	bootstrapSliceTime = 30 * time.Second
	bootstrapSliceSize = int64(512 << 20)
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type config struct {
	home          string
	confirm       string
	observeAppend time.Duration
	tempParent    string
}

type summary struct {
	Version               string `json:"version"`
	Result                string `json:"result"`
	SourceFilesBefore     int    `json:"sourceFilesBefore"`
	SourceFilesAfter      int    `json:"sourceFilesAfter"`
	SourceIssues          int    `json:"sourceIssues"`
	UnchangedSources      int    `json:"unchangedSources"`
	GrownSources          int    `json:"grownSources"`
	AddedSources          int    `json:"addedSources"`
	MovedSources          int    `json:"movedSources"`
	IncrementalObserved   bool   `json:"incrementalObserved"`
	BootstrapState        string `json:"bootstrapState"`
	BootstrapPhase        string `json:"bootstrapPhase"`
	FullHistoryReady      bool   `json:"fullHistoryReady"`
	FirstScreenMS         int64  `json:"firstScreenMs"`
	BootstrapMS           int64  `json:"bootstrapMs"`
	BootstrapBytes        int64  `json:"bootstrapBytes"`
	SessionMatched        int64  `json:"sessionMatched"`
	SessionPageItems      int    `json:"sessionPageItems"`
	ProjectMatched        int64  `json:"projectMatched"`
	ProjectPageItems      int    `json:"projectPageItems"`
	ObservedModelItems    int    `json:"observedModelItems"`
	LedgerTurns           int64  `json:"ledgerTurns"`
	LedgerPricedTurns     int64  `json:"ledgerPricedTurns"`
	LedgerUnpricedTurns   int64  `json:"ledgerUnpricedTurns"`
	LedgerTotalTokens     int64  `json:"ledgerTotalTokens"`
	LedgerUSDMicros       int64  `json:"ledgerUsdMicros"`
	PricingVersions       int    `json:"pricingVersions"`
	LedgerDifferenceZero  bool   `json:"ledgerDifferenceZero"`
	QuotaWindows          int    `json:"quotaWindows"`
	QuotaSources          int    `json:"quotaSources"`
	TrayRows              int    `json:"trayRows"`
	PrivacyTablesScanned  int    `json:"privacyTablesScanned"`
	PrivacyStringsScanned int64  `json:"privacyStringsScanned"`
	DTOPrivacyPassed      bool   `json:"dtoPrivacyPassed"`
	SourceReadOnlyPassed  bool   `json:"sourceReadOnlyPassed"`
	IsolatedStoreClosed   bool   `json:"isolatedStoreClosed"`
	IsolatedRootRemoved   bool   `json:"isolatedRootRemoved"`
}

type runArtifacts struct {
	root     string
	database *storesqlite.Store
}

func main() {
	input := config{}
	flag.StringVar(&input.home, "home", "", "explicit confirmed Codex Home")
	flag.StringVar(&input.confirm, "confirm", "", "read-only confirmation phrase")
	flag.DurationVar(&input.observeAppend, "observe-append", 20*time.Second, "bounded append observation window")
	flag.StringVar(&input.tempParent, "temp-parent", "", "optional private test parent")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := execute(ctx, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, stableFailure(err))
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "M11-LIVE-099: encode content-free summary")
		os.Exit(1)
	}
}

func execute(ctx context.Context, input config) (summary, error) {
	if err := validateConfig(input); err != nil {
		return summary{}, err
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, input.home)
	if err != nil {
		return summary{}, errors.Join(errStagePreflight, err)
	}
	discoverer, err := logs.NewConfirmedDiscoverer(metadata.Path, metadata.DeviceID, metadata.Inode)
	if err != nil {
		return summary{}, errors.Join(errStagePreflight, err)
	}
	before, err := discoverer.Discover(ctx)
	if err != nil || len(before.Issues) != 0 {
		return summary{}, errors.Join(errStagePreflight, err, errSourceIssue)
	}

	artifacts, err := openArtifacts(ctx, input.tempParent)
	if err != nil {
		return summary{}, errors.Join(errStageIsolatedStore, err)
	}
	closed := false
	removed := false
	defer func() {
		if !closed && artifacts.database != nil {
			_ = artifacts.database.Close(context.Background())
		}
		if !removed && artifacts.root != "" {
			_ = removeIsolatedRoot(artifacts.root)
		}
	}()

	repository := factstore.NewRepository(artifacts.database)
	if _, err := repository.MigrateApplicationSchema(ctx); err != nil {
		return summary{}, errors.Join(errStageIsolatedStore, err)
	}
	catalog := pricing.BuiltinOpenAI20260714()
	if err := repository.AddPricingVersion(ctx, catalog); err != nil {
		return summary{}, errors.Join(errStageIsolatedStore, err)
	}
	runtime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err != nil {
		return summary{}, errors.Join(errStageBootstrap, err)
	}
	request := preferences.BootstrapRequest{
		SwitchID: "m11-real-home-validation", Generation: 1,
		Source: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
		DataStoreKey: "m11-isolated-store", Strategy: preferences.HomeSwitchIndependentDatabase,
	}
	if err := runtime.StartBootstrap(ctx, request); err != nil {
		return summary{}, errors.Join(errStageBootstrapStart, err)
	}
	job, _, err := repository.BootstrapRunByIdentity(ctx, request.SwitchID, int64(request.Generation))
	if err != nil {
		return summary{}, errors.Join(errStageBootstrapIdentity, err)
	}
	report, err := runBootstrapWithProgress(ctx, runtime, job.JobID, os.Stderr)
	if err != nil {
		return summary{}, errors.Join(errStageBootstrapRun, err)
	}

	after, plan, err := observeAndReconcile(ctx, metadata.Path, discoverer, before, input.observeAppend)
	if err != nil {
		return summary{}, errors.Join(errStageSourceReconcile, err)
	}
	result := summary{
		Version: contractVersion, Result: "passed", SourceFilesBefore: len(before.Snapshots),
		SourceFilesAfter: len(after.Snapshots), SourceIssues: len(after.Issues),
		BootstrapState: string(report.State), BootstrapPhase: string(report.Phase),
		FullHistoryReady: report.FullHistoryReady, FirstScreenMS: report.FirstScreenMS,
		BootstrapMS: report.BootstrapMS, BootstrapBytes: report.BytesRead, SourceReadOnlyPassed: true,
	}
	if !report.FullHistoryReady {
		return summary{}, errBootstrapIncomplete
	}
	if _, err := repository.RebuildCostLedger(ctx, factstore.RebuildCostLedgerRequest{
		GenerationID: "m11-real-home-utc-v1", ReportingTimezone: "UTC",
		PricingSource: catalog.Source, Currency: catalog.Currency, RollupVersion: 1,
		CalculatedAtMS: time.Now().UnixMilli(),
	}); err != nil {
		return summary{}, errors.Join(errStageCostRollup, err)
	}
	if err := classifyReconcile(plan, &result); err != nil {
		return summary{}, err
	}
	result.IncrementalObserved = hasAppendAction(plan)
	if input.observeAppend > 0 && !result.IncrementalObserved {
		return summary{}, errIncrementalNotObserved
	}

	usageService, err := usagecost.NewService(repository)
	if err != nil {
		return summary{}, errors.Join(errStagePublicQuerySetup, err)
	}
	sessions, err := usageService.ListSessions(ctx, basequery.Request{Page: basequery.PageRequest{Limit: 100}})
	if err != nil {
		return summary{}, errors.Join(errStageSessionQuery, err)
	}
	result.SessionMatched = numericValue(sessions.MatchedCount)
	result.SessionPageItems = len(sessions.Items)
	if result.SessionMatched < int64(result.SessionPageItems) || result.SessionMatched == 0 {
		return summary{}, errQueryMismatch
	}
	for _, item := range sessions.Items {
		if item.Model.ID != nil {
			result.ObservedModelItems++
		}
	}

	rangeRequest := lastYearRange(time.Now().UTC())
	projects, err := usageService.ListProjects(ctx, basequery.Request{
		Page: basequery.PageRequest{Limit: 100}, TimeRange: &rangeRequest,
	})
	if err != nil {
		return summary{}, errors.Join(errStageProjectQuery, err)
	}
	result.ProjectMatched = numericValue(projects.MatchedCount)
	result.ProjectPageItems = len(projects.Items)
	if result.ProjectMatched < int64(result.ProjectPageItems) || result.ProjectMatched == 0 || result.ObservedModelItems == 0 {
		return summary{}, errQueryMismatch
	}
	usage, err := usageService.UsageCost(ctx, usagecost.UsageCostRequest{
		Range: rangeRequest, Granularity: usagecost.TrendDay,
	})
	if err != nil || !sameKnownUsageTotals(usage.Totals, projects.GlobalTotals) {
		return summary{}, errQueryMismatch
	}
	result.LedgerTurns = numericValue(projects.GlobalTotals.TurnCount)
	result.LedgerPricedTurns = numericValue(projects.GlobalTotals.PricedTurnCount)
	result.LedgerUnpricedTurns = numericValue(projects.GlobalTotals.UnpricedTurnCount)
	result.LedgerTotalTokens = numericValue(projects.GlobalTotals.TotalTokens)
	result.LedgerUSDMicros = numericValue(projects.GlobalTotals.EstimatedUSDMicros)
	result.PricingVersions = len(projects.PricingVersions)
	result.LedgerDifferenceZero = result.LedgerTurns == result.LedgerPricedTurns+result.LedgerUnpricedTurns
	if result.LedgerTurns <= 0 || !result.LedgerDifferenceZero || result.PricingVersions == 0 {
		return summary{}, errQueryMismatch
	}

	quotaService, err := quotaquery.NewCurrentQueryService(repository)
	if err != nil {
		return summary{}, errors.Join(errStageQuota, err)
	}
	quota, quotaErr := quotaService.Query(ctx, time.Now().UnixMilli())
	if quotaErr != nil || len(quota.Windows) == 0 {
		return summary{}, errQuotaUnavailable
	}
	result.QuotaWindows = len(quota.Windows)
	result.QuotaSources = len(quota.Sources)
	traySnapshot := platformtray.Snapshot{Windows: make([]platformtray.WindowSnapshot, 0, len(quota.Windows))}
	for _, window := range quota.Windows {
		var kind platformtray.WindowKind
		switch window.WindowKind {
		case factstore.QuotaWindowPrimary:
			kind = platformtray.WindowPrimary
		case factstore.QuotaWindowSecondary:
			kind = platformtray.WindowSecondary
		default:
			return summary{}, errQueryMismatch
		}
		traySnapshot.Windows = append(traySnapshot.Windows, platformtray.WindowSnapshot{
			Kind: kind, RemainingPercent: window.RemainingPercent,
			ResetRemainingMS: window.ResetRemainingMS,
			Freshness:        platformtray.Freshness(window.Freshness),
			Conflict:         window.Conflict == factstore.QuotaConflictPresent,
		})
	}
	result.TrayRows = len(platformtray.NewProjector().Project(traySnapshot).Rows)

	privacy, err := scanDatabasePrivacy(ctx, artifacts.database)
	if err != nil {
		return summary{}, errors.Join(errStagePrivacy, err)
	}
	result.PrivacyTablesScanned = privacy.tables
	result.PrivacyStringsScanned = privacy.strings
	if err := scanDTOs(metadata.Path, sessions, projects, quota); err != nil {
		return summary{}, errors.Join(errStagePrivacy, err)
	}
	result.DTOPrivacyPassed = true

	if err := artifacts.database.Close(context.Background()); err != nil {
		return summary{}, fmt.Errorf("close store: %w", err)
	}
	closed = true
	result.IsolatedStoreClosed = true
	if err := removeIsolatedRoot(artifacts.root); err != nil {
		return summary{}, err
	}
	removed = true
	result.IsolatedRootRemoved = true
	return result, nil
}

type bootstrapSliceRunner interface {
	RunSlice(context.Context, string, bootstrap.SliceBudget) (bootstrap.SliceReport, error)
}

type bootstrapProgress struct {
	bootstrap.RunReport
	BytesRead     int64
	FirstScreenMS int64
	BootstrapMS   int64
}

func runBootstrapWithProgress(
	ctx context.Context,
	runner bootstrapSliceRunner,
	jobID string,
	progress io.Writer,
) (bootstrapProgress, error) {
	if runner == nil || jobID == "" || progress == nil {
		return bootstrapProgress{}, errInvalidConfig
	}
	started := time.Now()
	var totalBytes int64
	var firstScreenMS int64
	for slice := 1; ; slice++ {
		report, err := runner.RunSlice(ctx, jobID, bootstrap.SliceBudget{
			MaxFiles: 1_000_000, MaxBytes: bootstrapSliceSize, MaxActive: bootstrapSliceTime,
		})
		if err != nil {
			return bootstrapProgress{RunReport: report.RunReport}, err
		}
		totalBytes += report.BytesRead
		if firstScreenMS == 0 && report.FirstScreenReady {
			firstScreenMS = time.Since(started).Milliseconds()
		}
		if _, err := fmt.Fprintf(
			progress,
			"M11-LIVE-PROGRESS slice=%d files=%d slice_bytes=%d total_bytes=%d active_ms=%d state=%s phase=%s stop=%s complete=%t\n",
			slice, report.FilesProcessed, report.BytesRead, totalBytes, report.Active.Milliseconds(),
			report.State, report.Phase, report.ExhaustedBy, report.Complete,
		); err != nil {
			return bootstrapProgress{RunReport: report.RunReport}, err
		}
		if report.Complete {
			return bootstrapProgress{
				RunReport: report.RunReport, BytesRead: totalBytes,
				FirstScreenMS: firstScreenMS, BootstrapMS: time.Since(started).Milliseconds(),
			}, nil
		}
	}
}

func validateConfig(input config) error {
	if input.confirm != confirmationPhrase || input.home == "" || !filepath.IsAbs(input.home) ||
		filepath.Clean(input.home) != input.home || input.observeAppend < 0 || input.observeAppend > time.Minute {
		return errInvalidConfig
	}
	if input.tempParent != "" && (!filepath.IsAbs(input.tempParent) || filepath.Clean(input.tempParent) != input.tempParent) {
		return errInvalidConfig
	}
	if input.tempParent != "" {
		home, err := filepath.EvalSymlinks(input.home)
		if err != nil {
			return errInvalidConfig
		}
		parent, err := filepath.EvalSymlinks(input.tempParent)
		if err != nil || pathWithin(parent, home) {
			return errInvalidConfig
		}
	}
	defaultPath, err := storesqlite.DefaultPath()
	if err == nil && strings.HasPrefix(defaultPath, input.home+string(os.PathSeparator)) {
		return errInvalidConfig
	}
	return nil
}

func pathWithin(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func openArtifacts(ctx context.Context, parent string) (runArtifacts, error) {
	root, err := os.MkdirTemp(parent, "codex-pulse-m11-real-")
	if err != nil {
		return runArtifacts{}, fmt.Errorf("create isolated root: %w", err)
	}
	opened := false
	defer func() {
		if !opened {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return runArtifacts{}, fmt.Errorf("secure isolated root: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".m11-real-home-root"), []byte(isolationMarker), 0o600); err != nil {
		return runArtifacts{}, fmt.Errorf("mark isolated root: %w", err)
	}
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(root, "codex-pulse.db")})
	if err != nil {
		return runArtifacts{}, fmt.Errorf("open isolated store: %w", err)
	}
	opened = true
	return runArtifacts{root: root, database: database}, nil
}

func removeIsolatedRoot(root string) error {
	base := filepath.Base(root)
	if !filepath.IsAbs(root) || !strings.HasPrefix(base, "codex-pulse-m11-real-") || base == "codex-pulse-m11-real-" {
		return errUnsafeCleanup
	}
	markerPath := filepath.Join(root, ".m11-real-home-root")
	info, err := os.Lstat(markerPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errUnsafeCleanup
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != isolationMarker {
		return errUnsafeCleanup
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove isolated root: %w", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		return errUnsafeCleanup
	}
	return nil
}

func observeAndReconcile(
	ctx context.Context,
	home string,
	discoverer *logs.Discoverer,
	before logs.DiscoveryResult,
	window time.Duration,
) (logs.DiscoveryResult, logs.ReconcilePlan, error) {
	deadline := time.Now().Add(window)
	for {
		after, err := discoverer.DiscoverAgainst(ctx, before.Snapshots)
		if err != nil || len(after.Issues) != 0 {
			return logs.DiscoveryResult{}, logs.ReconcilePlan{}, fmt.Errorf("final discovery: %w", errors.Join(err, errSourceIssue))
		}
		plan, err := logs.PlanReconcile(home, before.Snapshots, after)
		if err != nil {
			return logs.DiscoveryResult{}, logs.ReconcilePlan{}, fmt.Errorf("source reconcile: %w", err)
		}
		if window == 0 || hasAppendAction(plan) || !time.Now().Before(deadline) {
			return after, plan, nil
		}
		select {
		case <-ctx.Done():
			return logs.DiscoveryResult{}, logs.ReconcilePlan{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func hasAppendAction(plan logs.ReconcilePlan) bool {
	for _, action := range plan.Actions {
		if action.Kind == logs.ChangeGrown || action.Kind == logs.ChangeAdded || action.Kind == logs.ChangeMoved {
			return true
		}
	}
	return false
}

func classifyReconcile(plan logs.ReconcilePlan, result *summary) error {
	for _, action := range plan.Actions {
		switch action.Kind {
		case logs.ChangeUnchanged:
			result.UnchangedSources++
		case logs.ChangeGrown:
			result.GrownSources++
		case logs.ChangeAdded:
			result.AddedSources++
		case logs.ChangeMoved:
			result.MovedSources++
		default:
			return errSourceMutation
		}
	}
	return nil
}

func lastYearRange(now time.Time) basequery.LocalDateRange {
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -365)
	return basequery.LocalDateRange{
		StartDate: start.Format("2006-01-02"), EndDateExclusive: end.Format("2006-01-02"), TimeZone: "UTC",
	}
}

func numericValue(value basequery.NumericValue) int64 {
	if value.Value == nil {
		return 0
	}
	return *value.Value
}

func sameKnownUsageTotals(left, right usagecost.UsageTotals) bool {
	pairs := [][2]basequery.NumericValue{
		{left.TurnCount, right.TurnCount},
		{left.InputTokens, right.InputTokens},
		{left.CachedInputTokens, right.CachedInputTokens},
		{left.OutputTokens, right.OutputTokens},
		{left.ReasoningTokens, right.ReasoningTokens},
		{left.TotalTokens, right.TotalTokens},
		{left.EstimatedUSDMicros, right.EstimatedUSDMicros},
		{left.PricedTurnCount, right.PricedTurnCount},
		{left.UnpricedTurnCount, right.UnpricedTurnCount},
	}
	for _, pair := range pairs {
		if pair[0].Value == nil || pair[1].Value == nil || pair[0].Unit != pair[1].Unit ||
			*pair[0].Value != *pair[1].Value {
			return false
		}
	}
	return true
}

type privacyResult struct {
	tables  int
	strings int64
}

type privacyRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

func scanPrivacyRows(rows privacyRows, result *privacyResult) error {
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			_ = rows.Close()
			return err
		}
		if value.Valid {
			result.strings++
			if containsSensitiveEnvelope(value.String) {
				_ = rows.Close()
				return errPrivacyContract
			}
		}
	}
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	return closeErr
}

func scanDatabasePrivacy(ctx context.Context, database *storesqlite.Store) (privacyResult, error) {
	result := privacyResult{}
	forbiddenColumns := map[string]struct{}{
		"prompt": {}, "response": {}, "content": {}, "tool_output": {}, "raw_json": {},
		"authorization": {}, "cookie": {}, "access_token": {}, "refresh_token": {},
	}
	err := database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		tables, err := connection.WithContext(ctx).Migrator().GetTables()
		if err != nil {
			return err
		}
		sort.Strings(tables)
		for _, table := range tables {
			if !safeIdentifier.MatchString(table) {
				return errPrivacyContract
			}
			result.tables++
			columns, err := connection.WithContext(ctx).Migrator().ColumnTypes(table)
			if err != nil {
				return err
			}
			for _, column := range columns {
				name := strings.ToLower(column.Name())
				if _, forbidden := forbiddenColumns[name]; forbidden {
					return errPrivacyContract
				}
				if !safeIdentifier.MatchString(column.Name()) || !isTextColumn(column.DatabaseTypeName()) {
					continue
				}
				rows, err := connection.WithContext(ctx).Table(table).Select(column.Name()).Rows()
				if err != nil {
					return err
				}
				if err := scanPrivacyRows(rows, &result); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return result, err
}

func isTextColumn(databaseType string) bool {
	typeName := strings.ToUpper(databaseType)
	return strings.Contains(typeName, "CHAR") || strings.Contains(typeName, "CLOB") || strings.Contains(typeName, "TEXT")
}

func containsSensitiveEnvelope(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "authorization: bearer ") || strings.Contains(lower, "cookie:") {
		return true
	}
	compact := strings.Map(func(value rune) rune {
		switch value {
		case ' ', '\t', '\r', '\n':
			return -1
		default:
			return value
		}
	}, lower)
	for _, forbidden := range []string{
		"\"authorization\":", "\"cookie\":",
		"\"access_token\":", "\"refresh_token\":", "\"auth\":", "\"prompt\":",
		"\"response\":", "\"role\":\"user\"", "\"type\":\"response_item\"", "\"tool_output\":",
	} {
		if strings.Contains(compact, forbidden) {
			return true
		}
	}
	return false
}

func scanDTOs(home string, values ...any) error {
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		text := string(encoded)
		var decoded any
		if json.Unmarshal(encoded, &decoded) != nil || containsAbsolutePath(decoded) ||
			strings.Contains(text, home) || containsSensitiveEnvelope(text) {
			return errPrivacyContract
		}
	}
	return nil
}

func containsAbsolutePath(value any) bool {
	switch typed := value.(type) {
	case string:
		return filepath.IsAbs(strings.TrimSpace(typed))
	case []any:
		for _, item := range typed {
			if containsAbsolutePath(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsAbsolutePath(item) {
				return true
			}
		}
	}
	return false
}

func stableFailure(err error) string {
	var sqliteCode interface{ Code() int }
	if errors.As(err, &sqliteCode) {
		return fmt.Sprintf(
			"M11-LIVE-013SQL: bootstrap SQLite result code %d category %s",
			sqliteCode.Code(), allowlistedSQLiteCategory(err),
		)
	}
	switch {
	case errors.Is(err, errInvalidConfig):
		return "M11-LIVE-010: invalid opt-in configuration"
	case errors.Is(err, errStagePreflight):
		return "M11-LIVE-011: metadata preflight or initial discovery failed"
	case errors.Is(err, errStageIsolatedStore):
		return "M11-LIVE-012: isolated Store bootstrap failed"
	case errors.Is(err, bootstrap.ErrInvalidRequest):
		return "M11-LIVE-013A1: bootstrap request contract was rejected"
	case errors.Is(err, logs.ErrHomeChanged):
		return "M11-LIVE-013A2: confirmed Home identity changed"
	case errors.Is(err, logs.ErrChangedDuringScan):
		return "M11-LIVE-013A3: source changed during bootstrap plan freeze"
	case errors.Is(err, bootstrap.ErrDiscoveryIncomplete):
		return "M11-LIVE-013A4: bootstrap discovery contains blocking issues"
	case errors.Is(err, bootstrap.ErrSourceUnavailable):
		return "M11-LIVE-013A5: bootstrap source is unavailable"
	case errors.Is(err, logs.ErrUnsafeSource), errors.Is(err, logs.ErrUnsupportedFile):
		return "M11-LIVE-013A6: bootstrap source safety contract was rejected"
	case errors.Is(err, logs.ErrInvalidSnapshot):
		return "M11-LIVE-013A7: bootstrap snapshot contract was rejected"
	case errors.Is(err, bootstrap.ErrInvalidPlan):
		return "M11-LIVE-013A7P: bootstrap planner rejected real source metadata"
	case errors.Is(err, errStageBootstrapStart):
		return "M11-LIVE-013A: real Home bootstrap plan freeze failed"
	case errors.Is(err, errStageBootstrapIdentity):
		return "M11-LIVE-013B: bootstrap identity readback failed"
	case errors.Is(err, errStageBootstrapRun) && errors.Is(err, factstore.ErrInvalidRecord):
		return "M11-LIVE-013C8: bootstrap execution rejected fact category " + allowlistedFactCategory(err)
	case errors.Is(err, errStageBootstrapRun):
		return "M11-LIVE-013C: real Home bootstrap execution failed"
	case errors.Is(err, factstore.ErrInvalidRecord):
		return "M11-LIVE-013A8: bootstrap fact contract was rejected category " + allowlistedFactCategory(err)
	case errors.Is(err, errStageSourceReconcile):
		return "M11-LIVE-014: post-bootstrap source reconcile failed"
	case errors.Is(err, errStagePublicQuerySetup):
		return "M11-LIVE-015A: public usage query setup failed"
	case errors.Is(err, errStageSessionQuery):
		return "M11-LIVE-015B: public session query failed category " + publicQueryCategory(err) +
			" detail " + allowlistedErrorChainCategory(err)
	case errors.Is(err, errStageProjectQuery):
		return "M11-LIVE-015C: public project query failed category " + publicQueryCategory(err) +
			" detail " + allowlistedErrorChainCategory(err)
	case errors.Is(err, errStageCostRollup):
		return "M11-LIVE-015D: UTC cost rollup failed detail " + allowlistedErrorChainCategory(err)
	case errors.Is(err, errStageQuota):
		return "M11-LIVE-016: local quota query setup failed"
	case errors.Is(err, errStagePrivacy):
		return "M11-LIVE-017: privacy scan execution failed"
	case errors.Is(err, errSourceIssue), errors.Is(err, errSourceMutation):
		return "M11-LIVE-020: confirmed source safety check failed"
	case errors.Is(err, errIncrementalNotObserved):
		return "M11-LIVE-025: bounded incremental append was not observed"
	case errors.Is(err, errBootstrapIncomplete):
		return "M11-LIVE-030: bootstrap did not reach full history ready"
	case errors.Is(err, errQueryMismatch):
		return "M11-LIVE-040: public query reconciliation failed"
	case errors.Is(err, errQuotaUnavailable):
		return "M11-LIVE-045: local quota projection is unavailable"
	case errors.Is(err, errPrivacyContract):
		return "M11-LIVE-050: privacy contract failed"
	case errors.Is(err, errUnsafeCleanup):
		return "M11-LIVE-060: isolated cleanup contract failed"
	default:
		return "M11-LIVE-090: live validation failed"
	}
}

func allowlistedFactCategory(err error) string {
	lower := strings.ToLower(err.Error())
	patterns := []struct {
		needle, category string
	}{
		{"bootstrap plan item is invalid", "plan_item"},
		{"bootstrap item total does not match", "plan_item_total"},
		{"bootstrap absence item", "plan_absence_total"},
		{"bootstrap plan total overflows", "plan_total_overflow"},
		{"bootstrap plan identity or time", "plan_identity"},
		{"bootstrap plan time does not advance", "plan_time"},
		{"bootstrap plan requires an active discover job", "plan_job_state"},
		{"bootstrap facts do not exist", "facts_missing"},
		{"bootstrap facts are invalid", "facts_invalid"},
		{"bootstrap plan state and digest", "facts_digest"},
		{"bootstrap facts regress or change immutable identity", "fact_transition"},
		{"bootstrap advance facts", "advance_facts"},
		{"job transition identity, state, phase, or time", "job_transition_shape"},
		{"job transition expected state is stale", "job_expected_state"},
		{"job state transition is illegal", "job_state"},
		{"job transition time does not advance", "job_time"},
		{"job phase regresses", "job_phase"},
		{"job progress regresses", "job_progress"},
		{"failed job requires an error class", "job_failed_error"},
		{"running or succeeded job must not have an error class", "job_running_error"},
		{"job progress exceeds total", "job_progress_total"},
		{"job transition", "job_transition"},
		{"session timestamps are invalid", "session_timestamps"},
		{"turn completion time or source offset is invalid", "turn_completion_time"},
		{"turn completion fields must be provided together", "turn_completion_shape"},
		{"turn identity or source position is invalid", "turn_identity"},
		{"turn facts conflict at the same source identity", "turn_fact_conflict"},
		{"turn completion conflicts at the same source identity", "turn_completion_conflict"},
		{"turn source offset conflicts within the same generation", "turn_source_position"},
		{"turn usage identity or timestamps are invalid", "turn_usage_identity"},
		{"turn usage conflicts at the same source position", "turn_usage_conflict"},
		{"turn usage source position regresses within the same generation", "turn_usage_position"},
		{"session current identity or timestamp is invalid", "session_current_identity"},
		{"session current conflicts at the same update time", "session_current_conflict"},
		{"session usage current identity or counters are invalid", "session_usage_identity"},
		{"session usage conflicts at the same counter position", "session_usage_conflict"},
		{"source file position or timestamps are invalid", "source_file_position"},
		{"source file cursor regresses within a generation", "source_cursor_regression"},
		{"source file update time regresses", "source_update_regression"},
		{"ingest diagnostic is invalid", "ingest_diagnostic"},
		{"project timestamps are invalid", "project_timestamps"},
		{"session analytics attribution is incomplete", "session_attribution_incomplete"},
		{"stored session turn attribution is invalid", "session_turn_attribution"},
		{"stored session turn model attribution is invalid", "session_turn_model_attribution"},
		{"stored session turn usage is invalid", "session_turn_usage"},
		{"stored session turn cost is invalid", "session_turn_cost"},
		{"stored session turn pricing evidence is invalid", "session_turn_pricing"},
		{"stored session rollup shape is invalid", "session_rollup"},
		{"project analytics attribution is incomplete", "project_attribution_incomplete"},
		{"project analytics global reconciliation failed", "project_global_reconciliation"},
		{"project contribution reconciliation failed", "project_contribution_reconciliation"},
		{"project list contribution reconciliation failed", "project_list_reconciliation"},
		{"analytics rollup is unavailable", "analytics_rollup_unavailable"},
		{"cost rebuild", "cost_rebuild"},
		{"cost rollup", "cost_rollup"},
		{"fact batch contains multiple session identities", "fact_session_identity"},
		{"stored bootstrap", "stored_fact"},
		{"bootstrap plan replay", "plan_replay"},
		{"source position already belongs to another turn", "turn_source_position_occupied"},
		{"active turn belongs to another session", "active_turn_session"},
		{"active turn does not exist", "active_turn_missing"},
		{"active turn does not match batch turn", "active_turn_batch"},
		{"turn belongs to another session", "turn_session"},
		{"turn session does not match batch session", "turn_batch_session"},
		{"turn project does not match batch project", "turn_batch_project"},
		{"turn usage belongs to another session", "usage_session"},
		{"turn usage generation does not match stored turn", "usage_generation"},
		{"session provider or source kind conflicts", "session_source_kind"},
		{"session belongs to another active source generation", "session_generation"},
		{"session project does not match batch project", "session_batch_project"},
		{"quota observation source position does not advance", "quota_position_non_monotonic"},
		{"quota observation time regresses", "quota_time_regression"},
		{"quota observation ID conflicts with stored sample", "quota_id_conflict"},
		{"quota observation does not match batch session", "quota_batch_session"},
		{"ingest quota observation does not match source file", "quota_batch_source"},
		{"local quota observation provenance is invalid", "quota_local_provenance"},
		{"online quota observation request is missing", "quota_online_provenance"},
		{"accepted quota observation is not trustworthy", "quota_untrusted_accepted"},
		{"non-accepted quota observation needs an allowlisted reason", "quota_rejection_reason"},
		{"quota observation sample is invalid", "quota_sample"},
		{"quota observation plan type is invalid", "quota_plan_type"},
		{"quota observation", "quota_observation_other"},
		{"stored quota", "stored_quota"},
		{"parser checkpoint", "parser_checkpoint"},
		{"projector ", "projector_checkpoint"},
		{"ingest ", "ingest_other"},
		{"session ", "session_other"},
		{"turn ", "turn_other"},
		{"usage ", "usage_other"},
		{"project ", "project_other"},
		{"source ", "source_other"},
		{"generation ", "generation_other"},
		{"current projection", "current_projection"},
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern.needle) {
			return pattern.category
		}
	}
	return "other"
}

func allowlistedErrorChainCategory(err error) string {
	visited := 0
	var walk func(error) string
	walk = func(current error) string {
		if current == nil || visited >= 32 {
			return "other"
		}
		visited++
		if category := allowlistedFactCategory(current); specificErrorCategory(category) {
			return category
		}
		switch value := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range value.Unwrap() {
				if category := walk(child); category != "other" {
					return category
				}
			}
		case interface{ Unwrap() error }:
			return walk(value.Unwrap())
		}
		return "other"
	}
	return walk(err)
}

func specificErrorCategory(category string) bool {
	switch category {
	case "other", "ingest_other", "session_other", "turn_other", "usage_other",
		"project_other", "source_other", "generation_other", "quota_observation_other", "cost_rollup":
		return false
	default:
		return true
	}
}

func allowlistedSQLiteCategory(err error) string {
	lower := strings.ToLower(err.Error())
	patterns := []struct {
		needle, category string
	}{
		{"too many sql variables", "variable_limit"},
		{"too many terms", "term_limit"},
		{"statement too long", "statement_limit"},
		{"string or blob too big", "value_limit"},
		{"constraint failed", "constraint"},
		{"unique constraint", "constraint"},
		{"no such table", "missing_table"},
		{"no such column", "missing_column"},
		{"syntax error", "syntax"},
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern.needle) {
			return pattern.category
		}
	}
	return "other"
}

func publicQueryCategory(err error) string {
	envelope, ok := basequery.ErrorEnvelopeFrom(err)
	if !ok {
		return "internal"
	}
	return string(envelope.Error.Code)
}

var (
	errInvalidConfig          = errors.New("invalid opt-in configuration")
	errSourceIssue            = errors.New("source discovery issue")
	errSourceMutation         = errors.New("source mutation detected")
	errIncrementalNotObserved = errors.New("incremental append not observed")
	errBootstrapIncomplete    = errors.New("bootstrap incomplete")
	errQueryMismatch          = errors.New("query reconciliation mismatch")
	errQuotaUnavailable       = errors.New("local quota projection unavailable")
	errPrivacyContract        = errors.New("privacy contract violation")
	errUnsafeCleanup          = errors.New("unsafe isolated cleanup")
	errStagePreflight         = errors.New("preflight stage")
	errStageIsolatedStore     = errors.New("isolated store stage")
	errStageBootstrap         = errors.New("bootstrap setup stage")
	errStageBootstrapStart    = errors.New("bootstrap start stage")
	errStageBootstrapIdentity = errors.New("bootstrap identity stage")
	errStageBootstrapRun      = errors.New("bootstrap run stage")
	errStageSourceReconcile   = errors.New("source reconcile stage")
	errStagePublicQuerySetup  = errors.New("public query setup stage")
	errStageSessionQuery      = errors.New("public session query stage")
	errStageProjectQuery      = errors.New("public project query stage")
	errStageCostRollup        = errors.New("UTC cost rollup stage")
	errStageQuota             = errors.New("quota stage")
	errStagePrivacy           = errors.New("privacy stage")
)
