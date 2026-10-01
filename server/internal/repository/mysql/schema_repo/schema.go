package schema_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

var ErrSchemaMissing = errors.New("center schema is missing: run explicit db init")
var ErrSchemaIncompatible = errors.New("center schema is incompatible: inspect structure before upgrade")

// Schema 不自动修改结构；服务启动仅检查，初始化只能由受控 CLI 调用。
type Schema struct{ engine *gormv2.Engine }

func NewSchema(engine *gormv2.Engine) *Schema { return &Schema{engine: engine} }

func (s *Schema) Check(ctx context.Context) error {
	db := s.engine.DB(ctx)
	if !db.Migrator().HasTable("pulse_schema") {
		return ErrSchemaMissing
	}
	var current schema_do.SchemaVersion
	err := db.Where("id = ?", 1).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSchemaMissing
	}
	if err != nil {
		return fmt.Errorf("read schema marker: %w", err)
	}
	_, checksum, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	if current.Version != schema.Version || current.Checksum != checksum {
		return ErrSchemaIncompatible
	}
	return s.checkColumns(ctx)
}

func (s *Schema) checkColumns(ctx context.Context) error {
	db := s.engine.DB(ctx)
	_, _, tables, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	for table, expected := range tables {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("%w: missing table %s", ErrSchemaIncompatible, table)
		}
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return fmt.Errorf("inspect schema columns: %w", err)
		}
		if len(columns) != len(expected) {
			return fmt.Errorf("%w: unexpected columns in %s", ErrSchemaIncompatible, table)
		}
		for _, column := range columns {
			if !slices.Contains(expected, column.Name()) {
				return fmt.Errorf("%w: unexpected column in %s", ErrSchemaIncompatible, table)
			}
		}
	}
	return nil
}

// Init 初始化已知结构，可安全重入已完成结构，不降级、不删除业务数据。
func (s *Schema) Init(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db := s.engine.DB(ctx)
	if db.Migrator().HasTable("pulse_schema") {
		var marker schema_do.SchemaVersion
		err := db.Where("id = ?", 1).Take(&marker).Error
		if err == nil {
			return s.Check(ctx)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	statements, checksum, _, err := schema.Definition(db.Dialector.Name())
	if err != nil {
		return err
	}
	initialize := func(ctx context.Context) error {
		for _, statement := range statements {
			if err := s.engine.DB(ctx).Exec(statement).Error; err != nil {
				return fmt.Errorf("initialize center structure: %w", err)
			}
		}
		if err := s.checkColumns(ctx); err != nil {
			return err
		}
		return s.engine.DB(ctx).Create(&schema_do.SchemaVersion{ID: 1, Version: schema.Version, Checksum: checksum, InitializedAtMS: time.Now().UnixMilli()}).Error
	}
	if db.Dialector.Name() == "sqlite" {
		return s.engine.Transaction(ctx, initialize)
	}
	// MySQL DDL 会隐式提交；标记放在全部 DDL 和读回成功之后，不冒充可事务回滚。
	return initialize(ctx)
}
