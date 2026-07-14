package store

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
)

var errLastInsertIDUnsupported = errors.New("GORM test bridge does not expose last insert ID")

// gormSQLResult 仅为 schema 故障注入测试保留 database/sql 的 RowsAffected 语义。
type gormSQLResult struct {
	rowsAffected int64
}

func (result gormSQLResult) LastInsertId() (int64, error) {
	return 0, errLastInsertIDUnsupported
}

func (result gormSQLResult) RowsAffected() (int64, error) {
	return result.rowsAffected, nil
}

// 下列 helper 只服务 PRAGMA、schema fault injection 与 EXPLAIN 测试。
func rawExec(ctx context.Context, database *gorm.DB, query string, arguments ...any) (sql.Result, error) {
	result := database.WithContext(ctx).Exec(query, arguments...)
	if result.Error != nil {
		return nil, result.Error
	}
	return gormSQLResult{rowsAffected: result.RowsAffected}, nil
}

func rawQueryRow(ctx context.Context, database *gorm.DB, query string, arguments ...any) *sql.Row {
	return database.WithContext(ctx).Raw(query, arguments...).Row()
}

func rawQueryRows(ctx context.Context, database *gorm.DB, query string, arguments ...any) (*sql.Rows, error) {
	return database.WithContext(ctx).Raw(query, arguments...).Rows()
}
