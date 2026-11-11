package schema_repo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
)

// queryIndexes 只解析仓库受控的独立 CREATE INDEX；SQL 仍是列序的唯一事实源。
func (s *Schema) queryIndexes(ctx context.Context, create bool) error {
	db := s.engine.DB(ctx)
	statements, _, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	for _, ddl := range statements {
		if !strings.HasPrefix(ddl, "CREATE INDEX idx_") {
			continue
		}
		head, tail, ok := strings.Cut(ddl, " (")
		words := strings.Fields(head)
		if !ok || len(words) != 5 || !strings.HasSuffix(tail, ")") {
			return fmt.Errorf("invalid managed index definition")
		}
		name, table := words[2], words[4]
		if create && !db.Migrator().HasIndex(table, name) {
			if err := db.Exec(ddl).Error; err != nil {
				return fmt.Errorf("create query index %s: %w", name, err)
			}
		}
		indexes, err := db.Migrator().GetIndexes(table)
		if err != nil {
			return fmt.Errorf("read query indexes: %w", err)
		}
		found := false
		for _, index := range indexes {
			if index.Name() == name {
				unique, known := index.Unique()
				found = known && !unique && slices.Equal(index.Columns(), strings.Split(strings.TrimSuffix(tail, ")"), ","))
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: query index %s", ErrSchemaIncompatible, name)
		}
	}
	return nil
}
