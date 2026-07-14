package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 ClassifyRuntimeError 只返回稳定 allowlisted class，不传播错误正文。
func TestClassifyRuntimeErrorReturnsOnlyStableClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want RuntimeErrorClass
	}{
		{"nil", nil, RuntimeErrorUnknown},
		{"deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), RuntimeErrorTimeout},
		{"cancelled", fmt.Errorf("wrapped: %w", context.Canceled), RuntimeErrorCanceled},
		{"busy", fmt.Errorf("wrapped: %w", storesqlite.ErrBusy), RuntimeErrorBusy},
		{"queue full", storesqlite.ErrQueueFull, RuntimeErrorBusy},
		{"disk full", storesqlite.ErrDiskFull, RuntimeErrorDiskFull},
		{"read only", storesqlite.ErrReadOnly, RuntimeErrorReadOnly},
		{"permission", storesqlite.ErrPermission, RuntimeErrorPermission},
		{"io", storesqlite.ErrIO, RuntimeErrorIO},
		{"corrupt", storesqlite.ErrCorrupt, RuntimeErrorCorrupt},
		{"closed", storesqlite.ErrClosed, RuntimeErrorUnavailable},
		{"invalid", storesqlite.ErrInvalidConfig, RuntimeErrorInvalid},
		{"unknown sensitive message", errors.New("Bearer synthetic-sensitive-value"), RuntimeErrorUnknown},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ClassifyRuntimeError(testCase.err); got != testCase.want {
				t.Fatalf("ClassifyRuntimeError() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// 测试 HealthEvent 按 fingerprint 去重并保留 resolve/reopen 与 source/job 关联。
func TestHealthEventDedupResolveReopenAndRelations(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	file := SourceFile{
		SourceFileID: "health-file", Provider: "codex", CurrentPath: "/synthetic/health.jsonl",
		DeviceID: "health-device", Inode: 7, SizeBytes: 10, MTimeNS: 10,
		ParsedOffset: 0, ParserVersion: "v1", State: SourceFileActive, UpdatedAtMS: 10,
	}
	if err := repository.UpsertSourceFile(context.Background(), file); err != nil {
		t.Fatalf("UpsertSourceFile() error = %v", err)
	}
	job := JobRun{
		JobID: "health-job", JobType: "scan", RequestedBy: "test", Priority: 1,
		State: JobQueued, Phase: JobPhaseDiscover, SourceFileID: pointerTo(file.SourceFileID),
		CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}

	observation := HealthObservation{
		EventID: "event-a", Fingerprint: SHA256DigestOf([]byte("source.timeout:health-file")), Domain: HealthDomainSource,
		Severity: HealthWarning, Code: HealthCodeSourceTimeout,
		SourceFileID: pointerTo(file.SourceFileID), JobID: pointerTo(job.JobID),
		ErrorClass: pointerTo(RuntimeErrorTimeout), ObservedAtMS: 100,
	}
	created, err := repository.ObserveHealthEvent(context.Background(), observation)
	if err != nil {
		t.Fatalf("ObserveHealthEvent() error = %v", err)
	}
	if created.OccurrenceCount != 1 || created.FirstSeenAtMS != 100 || created.LastSeenAtMS != 100 ||
		created.ResolvedAtMS != nil {
		t.Fatalf("created health event = %#v, want initial active occurrence", created)
	}
	for iteration := 0; iteration < 50; iteration++ {
		got, err := repository.ObserveHealthEvent(context.Background(), observation)
		if err != nil {
			t.Fatalf("ObserveHealthEvent(replay %d) error = %v", iteration, err)
		}
		if !reflect.DeepEqual(got, created) {
			t.Fatalf("replay %d = %#v, want %#v", iteration, got, created)
		}
	}

	second := observation
	second.Severity = HealthError
	second.ObservedAtMS = 110
	updated, err := repository.ObserveHealthEvent(context.Background(), second)
	if err != nil {
		t.Fatalf("ObserveHealthEvent(second) error = %v", err)
	}
	if updated.OccurrenceCount != 2 || updated.LastSeenAtMS != 110 || updated.Severity != HealthError {
		t.Fatalf("updated event = %#v, want merged second occurrence", updated)
	}
	if err := repository.ResolveHealthEvent(context.Background(), observation.EventID, 120); err != nil {
		t.Fatalf("ResolveHealthEvent() error = %v", err)
	}
	if err := repository.ResolveHealthEvent(context.Background(), observation.EventID, 120); err != nil {
		t.Fatalf("ResolveHealthEvent(replay) error = %v", err)
	}
	resolved, err := repository.HealthEvent(context.Background(), observation.EventID)
	if err != nil {
		t.Fatalf("HealthEvent(resolved) error = %v", err)
	}
	if resolved.ResolvedAtMS == nil || *resolved.ResolvedAtMS != 120 {
		t.Fatalf("resolved event = %#v, want resolved_at=120", resolved)
	}
	replayedAfterResolve, err := repository.ObserveHealthEvent(context.Background(), second)
	if err != nil {
		t.Fatalf("ObserveHealthEvent(exact replay after resolve) error = %v", err)
	}
	if !reflect.DeepEqual(replayedAfterResolve, resolved) {
		t.Fatalf("exact replay after resolve = %#v, want %#v", replayedAfterResolve, resolved)
	}
	lateArrivalBeforeResolution := second
	lateArrivalBeforeResolution.ObservedAtMS = 115
	if _, err := repository.ObserveHealthEvent(
		context.Background(), lateArrivalBeforeResolution,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(before resolution) error = %v, want ErrInvalidRecord", err)
	}
	unchanged, err := repository.HealthEvent(context.Background(), observation.EventID)
	if err != nil {
		t.Fatalf("HealthEvent(after rejected late arrival) error = %v", err)
	}
	if !reflect.DeepEqual(unchanged, resolved) {
		t.Fatalf("rejected late arrival mutated event: got %#v, want %#v", unchanged, resolved)
	}

	reopenedInput := second
	reopenedInput.ObservedAtMS = 130
	reopened, err := repository.ObserveHealthEvent(context.Background(), reopenedInput)
	if err != nil {
		t.Fatalf("ObserveHealthEvent(reopen) error = %v", err)
	}
	if reopened.ResolvedAtMS != nil || reopened.OccurrenceCount != 3 || reopened.LastSeenAtMS != 130 {
		t.Fatalf("reopened event = %#v, want active third occurrence", reopened)
	}

	conflict := reopenedInput
	conflict.Severity = HealthCritical
	if _, err := repository.ObserveHealthEvent(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(same-time conflict) error = %v, want ErrInvalidRecord", err)
	}
	older := reopenedInput
	older.ObservedAtMS = 129
	if _, err := repository.ObserveHealthEvent(context.Background(), older); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(older) error = %v, want ErrInvalidRecord", err)
	}
	identityConflict := reopenedInput
	identityConflict.EventID = "event-other"
	identityConflict.ObservedAtMS = 140
	if _, err := repository.ObserveHealthEvent(context.Background(), identityConflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(identity conflict) error = %v, want ErrInvalidRecord", err)
	}

	active := true
	events, err := repository.ListHealthEvents(context.Background(), HealthEventFilter{
		Active: &active, SourceFileID: pointerTo(file.SourceFileID), JobID: pointerTo(job.JobID), Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListHealthEvents() error = %v", err)
	}
	if !reflect.DeepEqual(events, []HealthEvent{reopened}) {
		t.Fatalf("ListHealthEvents() = %#v, want reopened event", events)
	}
	jobEvents, err := repository.ListHealthEvents(context.Background(), HealthEventFilter{
		JobID: pointerTo(job.JobID), Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListHealthEvents(job only) error = %v", err)
	}
	if !reflect.DeepEqual(jobEvents, []HealthEvent{reopened}) {
		t.Fatalf("ListHealthEvents(job only) = %#v, want reopened event", jobEvents)
	}
}

// 测试所有公开 HealthCode 常量同时通过 Repository allowlist 与 SQLite CHECK。
func TestHealthCodeAllowlistMatchesSchema(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	allowed := []struct {
		domain HealthDomain
		code   HealthCode
	}{
		{HealthDomainSource, HealthCodeSourceTimeout},
		{HealthDomainSource, HealthCodeSourceUnavailable},
		{HealthDomainSource, HealthCodeSourcePermission},
		{HealthDomainSource, HealthCodeSourceCorrupt},
		{HealthDomainSource, HealthCodeSourceStale},
		{HealthDomainJob, HealthCodeJobInterrupted},
		{HealthDomainJob, HealthCodeJobFailed},
		{HealthDomainJob, HealthCodeJobCancelled},
		{HealthDomainStore, HealthCodeStoreBusy},
		{HealthDomainStore, HealthCodeStoreDiskFull},
		{HealthDomainStore, HealthCodeStoreReadOnly},
		{HealthDomainStore, HealthCodeStorePermission},
		{HealthDomainStore, HealthCodeStoreIO},
		{HealthDomainStore, HealthCodeStoreCorrupt},
		{HealthDomainStore, HealthCodeStoreUnavailable},
		{HealthDomainStore, HealthCodeStoreUnknown},
		{HealthDomainPricing, HealthCodePricingUnavailable},
		{HealthDomainPricing, HealthCodePricingInvalid},
		{HealthDomainRuntime, HealthCodeRuntimeUnknown},
	}
	for index, testCase := range allowed {
		observation := HealthObservation{
			EventID:      fmt.Sprintf("allowlist-%02d", index),
			Fingerprint:  SHA256DigestOf([]byte(fmt.Sprintf("%s:%s", testCase.domain, testCase.code))),
			Domain:       testCase.domain,
			Severity:     HealthInfo,
			Code:         testCase.code,
			ObservedAtMS: int64(index),
		}
		created, err := repository.ObserveHealthEvent(context.Background(), observation)
		if err != nil {
			t.Fatalf("ObserveHealthEvent(%s, %s) error = %v", testCase.domain, testCase.code, err)
		}
		if created.Domain != testCase.domain || created.Code != testCase.code {
			t.Fatalf("ObserveHealthEvent(%s, %s) = %#v", testCase.domain, testCase.code, created)
		}
	}
}

// 测试 raw error/cursor/code 被 API 边界拒绝且不会进入 SQLite 文件字节。
func TestHealthPersistenceDoesNotContainRawSensitiveErrorText(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const secretMarker = "Bearer synthetic-sensitive-value"
	const identifierShapedSecret = "sk.proj_abc123"
	const fingerprintInput = "session_id=abc;token=secret"
	classified := ClassifyRuntimeError(errors.New(secretMarker))
	if classified != RuntimeErrorUnknown {
		t.Fatalf("ClassifyRuntimeError(synthetic secret) = %q, want unknown", classified)
	}
	if _, err := repository.ObserveHealthEvent(context.Background(), HealthObservation{
		EventID: "privacy-invalid", Fingerprint: SHA256DigestOf([]byte("privacy.invalid")), Domain: HealthDomainStore,
		Severity: HealthError, Code: HealthCode(secretMarker), ErrorClass: &classified, ObservedAtMS: 9,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(raw code) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.ObserveHealthEvent(context.Background(), HealthObservation{
		EventID: "privacy-token-code", Fingerprint: SHA256DigestOf([]byte("privacy.token-code")), Domain: HealthDomainStore,
		Severity: HealthError, Code: HealthCode(identifierShapedSecret), ErrorClass: &classified, ObservedAtMS: 9,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(identifier-shaped token code) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.ObserveHealthEvent(context.Background(), HealthObservation{
		EventID: "privacy-domain-code", Fingerprint: SHA256DigestOf([]byte("privacy.domain-code")), Domain: HealthDomainStore,
		Severity: HealthError, Code: HealthCodeSourceTimeout, ErrorClass: &classified, ObservedAtMS: 9,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ObserveHealthEvent(domain/code mismatch) error = %v, want ErrInvalidRecord", err)
	}
	_, err := repository.ObserveHealthEvent(context.Background(), HealthObservation{
		EventID: "privacy-event", Fingerprint: SHA256DigestOf([]byte(fingerprintInput)), Domain: HealthDomainStore,
		Severity: HealthError, Code: HealthCodeStoreUnknown, ErrorClass: &classified, ObservedAtMS: 10,
	})
	if err != nil {
		t.Fatalf("ObserveHealthEvent() error = %v", err)
	}

	var databasePath string
	err = repository.database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, `PRAGMA database_list`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sequence int
			var name, path string
			if err := rows.Scan(&sequence, &name, &path); err != nil {
				return err
			}
			if name == "main" {
				databasePath = path
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read database path: %v", err)
	}
	if databasePath == "" {
		t.Fatal("database path is empty")
	}
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		content, err := os.ReadFile(filepath.Clean(path))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read SQLite artifact %s: %v", filepath.Base(path), err)
		}
		for _, marker := range []string{secretMarker, identifierShapedSecret, fingerprintInput} {
			if bytes.Contains(content, []byte(marker)) {
				t.Fatalf("SQLite artifact %s contains raw sensitive marker", filepath.Base(path))
			}
		}
	}
}
