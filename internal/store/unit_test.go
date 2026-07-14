package store

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestWriteUnitCommitsFactsAndSourceCursorTogether(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	project, source := writeUnitFixtures()
	err := repository.WithinWriteUnit(context.Background(), func(unit *WriteUnit) error {
		if err := unit.UpsertFacts(FactBatch{Project: &project}); err != nil {
			return err
		}
		return unit.UpsertSourceFile(source)
	})
	if err != nil {
		t.Fatalf("WithinWriteUnit() error = %v", err)
	}
	assertWriteUnitRows(t, database, 1, 1)
}

func TestWriteUnitRollsBackFactsWhenSourceCursorFails(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	project, source := writeUnitFixtures()
	source.ParsedOffset = source.SizeBytes + 1
	err := repository.WithinWriteUnit(context.Background(), func(unit *WriteUnit) error {
		if err := unit.UpsertFacts(FactBatch{Project: &project}); err != nil {
			return err
		}
		return unit.UpsertSourceFile(source)
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("WithinWriteUnit() error = %v, want ErrInvalidRecord", err)
	}
	assertWriteUnitRows(t, database, 0, 0)
}

func TestWriteUnitCannotBeReusedAfterCallbackReturns(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	var captured *WriteUnit
	if err := repository.WithinWriteUnit(context.Background(), func(unit *WriteUnit) error {
		captured = unit
		return nil
	}); err != nil {
		t.Fatalf("WithinWriteUnit() error = %v", err)
	}
	project, source := writeUnitFixtures()
	if err := captured.UpsertFacts(FactBatch{Project: &project}); !errors.Is(err, ErrWriteUnitClosed) {
		t.Fatalf("captured.UpsertFacts() error = %v, want ErrWriteUnitClosed", err)
	}
	if err := captured.UpsertSourceFile(source); !errors.Is(err, ErrWriteUnitClosed) {
		t.Fatalf("captured.UpsertSourceFile() error = %v, want ErrWriteUnitClosed", err)
	}
}

func TestWriteUnitPreservesStoreRollbackForCallbackErrorPanicAndCancel(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		run  func(context.Context, context.CancelFunc, *WriteUnit) error
		want error
	}{
		{
			name: "callback error",
			run: func(_ context.Context, _ context.CancelFunc, unit *WriteUnit) error {
				project, _ := writeUnitFixtures()
				if err := unit.UpsertFacts(FactBatch{Project: &project}); err != nil {
					return err
				}
				return errors.New("injected callback failure")
			},
		},
		{
			name: "panic",
			run: func(_ context.Context, _ context.CancelFunc, unit *WriteUnit) error {
				project, _ := writeUnitFixtures()
				if err := unit.UpsertFacts(FactBatch{Project: &project}); err != nil {
					return err
				}
				panic("injected panic")
			},
			want: storesqlite.ErrCallbackPanic,
		},
		{
			name: "cancel",
			run: func(_ context.Context, cancel context.CancelFunc, unit *WriteUnit) error {
				project, _ := writeUnitFixtures()
				if err := unit.UpsertFacts(FactBatch{Project: &project}); err != nil {
					return err
				}
				cancel()
				return nil
			},
			want: storesqlite.ErrCanceled,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDatabase(t)
			repository := NewRepository(database)
			if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
				t.Fatalf("EnsureApplicationSchema() error = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
				return testCase.run(ctx, cancel, unit)
			})
			if testCase.want != nil && !errors.Is(err, testCase.want) {
				t.Fatalf("WithinWriteUnit() error = %v, want %v", err, testCase.want)
			}
			if testCase.want == nil && err == nil {
				t.Fatal("WithinWriteUnit() error = nil, want callback failure")
			}
			assertWriteUnitRows(t, database, 0, 0)
		})
	}
}

func writeUnitFixtures() (Project, SourceFile) {
	return Project{
			ProjectID: "unit-project", DisplayName: "Unit Project", RootPath: "/synthetic/unit",
			CreatedAtMS: 1, UpdatedAtMS: 1,
		}, SourceFile{
			SourceFileID: "unit-source", Provider: "codex", CurrentPath: "/synthetic/unit.jsonl",
			DeviceID: "device", Inode: 1, SizeBytes: 10, MTimeNS: 1, ParsedOffset: 5,
			ParserVersion: "v1", ActiveGeneration: 1, State: SourceFileActive, UpdatedAtMS: 1,
		}
}

func assertWriteUnitRows(t *testing.T, database *storesqlite.Store, wantProjects, wantSources int64) {
	t.Helper()
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		var projects int64
		if err := connection.WithContext(ctx).Model(&projectModel{}).Count(&projects).Error; err != nil {
			return err
		}
		var sources int64
		if err := connection.WithContext(ctx).Model(&sourceFileModel{}).Count(&sources).Error; err != nil {
			return err
		}
		if projects != wantProjects || sources != wantSources {
			t.Errorf("row counts projects=%d sources=%d, want %d/%d", projects, sources, wantProjects, wantSources)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect WriteUnit rows: %v", err)
	}
}
