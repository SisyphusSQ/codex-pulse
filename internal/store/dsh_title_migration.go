package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func currentDSHTitleSchemaObjects() []storeschema.Object {
	objects := append([]storeschema.Object(nil), dshProviderSchemaObjects...)
	for i := range objects {
		if objects[i].Name == "dsh_sessions" {
			objects[i].Statement = strings.Replace(objects[i].Statement, "length(display_title) BETWEEN 1 AND 128", "length(display_title) BETWEEN 1 AND 512", 1)
			objects[i].Statement = strings.Replace(objects[i].Statement, "('dsh_header','fallback')", "('dsh_header','dsh_title_event','fallback')", 1)
		}
	}
	return objects
}

func applicationSchemaV37Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, 37, "dsh-session-titles")
	for _, object := range currentDSHTitleSchemaObjects() {
		_, _ = fmt.Fprintln(hasher, object.ObjectType, object.Name, strings.TrimSpace(storeschema.NormalizeSQL(storeschema.CanonicalSQL(object.Statement))))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func migrateDSHTitlesForV37(ctx context.Context, transaction *gorm.DB) error {
	database := transaction.WithContext(ctx)
	// Preserve lineage before rebuilding its parent; keep FK enforcement enabled.
	for _, statement := range []string{
		"CREATE TEMP TABLE dsh_title_lineage_backup AS SELECT * FROM dsh_session_lineage",
		"DROP TABLE dsh_session_lineage",
		"ALTER TABLE dsh_sessions RENAME TO dsh_sessions_v36",
	} {
		if err := database.Exec(statement).Error; err != nil {
			return err
		}
	}
	objects := currentDSHTitleSchemaObjects()
	if err := storeschema.EnsureObjects(ctx, database, objects[:1]); err != nil {
		return err
	}
	for _, statement := range []string{
		"INSERT INTO dsh_sessions SELECT * FROM dsh_sessions_v36",
		"DROP TABLE dsh_sessions_v36",
	} {
		if err := database.Exec(statement).Error; err != nil {
			return err
		}
	}
	if err := storeschema.EnsureObjects(ctx, database, objects); err != nil {
		return err
	}
	if err := database.Exec("INSERT INTO dsh_session_lineage SELECT * FROM dsh_title_lineage_backup").Error; err != nil {
		return err
	}
	return database.Exec("DROP TABLE dsh_title_lineage_backup").Error
}
