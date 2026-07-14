package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestIngestSchemaColumnsForeignKeysIndexesAndPrivacyAreFrozen(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	wantColumns := map[string][]string{
		"source_generations": {
			"source_file_id", "generation", "state", "provider", "source_kind", "current_path",
			"device_id", "inode", "size_bytes", "mtime_ns", "prefix_bytes", "prefix_sha256",
			"fingerprint_sha256", "parser_version", "committed_offset", "session_id",
			"replaces_source_file_id", "base_source_file_id", "base_generation",
			"base_fingerprint_sha256", "superseded_building_source_file_id",
			"superseded_building_generation", "superseded_building_fingerprint_sha256",
			"superseded_building_parser_version", "updated_at_ms",
		},
		"parser_checkpoints": {
			"source_file_id", "generation", "checkpoint_version", "parser_seed", "projector_state", "updated_at_ms",
		},
		"source_generation_batches": {
			"source_file_id", "generation", "from_offset", "to_offset", "batch_identity_sha256", "facts", "eof", "created_at_ms",
		},
		"parser_diagnostics": {
			"source_file_id", "generation", "batch_end_offset", "batch_identity_sha256", "ordinal", "class", "code",
			"start_offset", "end_offset", "retryable",
		},
	}
	wantForeignKeys := []string{
		"parser_checkpoints.generation->source_generations.generation/CASCADE",
		"parser_checkpoints.source_file_id->source_generations.source_file_id/CASCADE",
		"parser_diagnostics.generation->source_generations.generation/CASCADE",
		"parser_diagnostics.source_file_id->source_generations.source_file_id/CASCADE",
		"source_generation_batches.generation->source_generations.generation/CASCADE",
		"source_generation_batches.source_file_id->source_generations.source_file_id/CASCADE",
		"source_generations.base_generation->source_generations.generation/RESTRICT",
		"source_generations.base_source_file_id->source_generations.source_file_id/RESTRICT",
		"source_generations.replaces_source_file_id->source_files.source_file_id/SET NULL",
		"source_generations.session_id->sessions.session_id/SET NULL",
		"source_generations.source_file_id->source_files.source_file_id/CASCADE",
		"source_generations.superseded_building_generation->source_generations.generation/RESTRICT",
		"source_generations.superseded_building_source_file_id->source_generations.source_file_id/RESTRICT",
	}
	wantIndexes := []string{
		"idx_generation_batches_replay", "idx_parser_diagnostics_source",
		"idx_source_generations_active", "idx_source_generations_active_session",
		"idx_source_generations_building", "idx_source_generations_snapshot",
	}
	var gotForeignKeys, gotIndexes []string
	err := database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for table, want := range wantColumns {
			rows, err := rawQueryRows(ctx, connection, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
			if err != nil {
				return err
			}
			var got []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					rows.Close()
					return err
				}
				got = append(got, name)
				lower := strings.ToLower(name)
				for _, forbidden := range []string{"raw_json", "prompt", "response", "tool_output", "error_text", "content"} {
					if strings.Contains(lower, forbidden) {
						t.Errorf("privacy-forbidden column %s.%s", table, name)
					}
				}
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if !equalStrings(got, want) {
				t.Errorf("%s columns = %v, want %v", table, got, want)
			}
			foreignRows, err := rawQueryRows(
				ctx, connection,
				`SELECT "from", "table", "to", on_delete FROM pragma_foreign_key_list(?)`, table,
			)
			if err != nil {
				return err
			}
			for foreignRows.Next() {
				var from, parent, to, onDelete string
				if err := foreignRows.Scan(&from, &parent, &to, &onDelete); err != nil {
					foreignRows.Close()
					return err
				}
				gotForeignKeys = append(gotForeignKeys, fmt.Sprintf(
					"%s.%s->%s.%s/%s", table, from, parent, to, onDelete,
				))
			}
			if err := foreignRows.Close(); err != nil {
				return err
			}
		}
		indexRows, err := rawQueryRows(ctx, connection, `
			SELECT name FROM sqlite_schema
			WHERE type = 'index' AND name IN (
				'idx_generation_batches_replay', 'idx_parser_diagnostics_source',
				'idx_source_generations_active', 'idx_source_generations_active_session',
				'idx_source_generations_building',
				'idx_source_generations_snapshot'
			)
			ORDER BY name
		`)
		if err != nil {
			return err
		}
		defer indexRows.Close()
		for indexRows.Next() {
			var name string
			if err := indexRows.Scan(&name); err != nil {
				return err
			}
			gotIndexes = append(gotIndexes, name)
		}
		return indexRows.Err()
	})
	if err != nil {
		t.Fatalf("inspect ingest schema: %v", err)
	}
	sort.Strings(gotForeignKeys)
	if !equalStrings(gotForeignKeys, wantForeignKeys) {
		t.Errorf("foreign keys = %v, want %v", gotForeignKeys, wantForeignKeys)
	}
	if !equalStrings(gotIndexes, wantIndexes) {
		t.Errorf("indexes = %v, want %v", gotIndexes, wantIndexes)
	}
}
