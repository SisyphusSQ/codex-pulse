// Package schema owns SQLite schema object verification shared by store domains.
package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ErrContract 表示既有数据库结构与当前 canonical schema 不兼容。
var ErrContract = errors.New("schema contract mismatch")

// Object 描述一个需要精确校验的 SQLite schema 对象。
type Object struct {
	ObjectType string
	Name       string
	Statement  string
}

// EnsureObjects 创建缺失对象，并拒绝覆盖与 canonical DDL 不一致的既有对象。
func EnsureObjects(ctx context.Context, transaction *gorm.DB, objects []Object) error {
	for _, object := range objects {
		if err := EnsureObject(ctx, transaction, object); err != nil {
			return err
		}
	}
	return nil
}

// EnsureObject 创建一个缺失对象，并在写入后重新校验。
func EnsureObject(ctx context.Context, transaction *gorm.DB, object Object) error {
	exists, err := VerifyObject(ctx, transaction, object)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := transaction.WithContext(ctx).Exec(object.Statement).Error; err != nil {
		return err
	}
	exists, err = VerifyObject(ctx, transaction, object)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %s %q was not created", ErrContract, object.ObjectType, object.Name)
	}
	return nil
}

// VerifyObject 精确比较 sqlite_schema 中的对象类型与 canonical DDL。
func VerifyObject(ctx context.Context, transaction *gorm.DB, object Object) (bool, error) {
	var actualType string
	var actualSQL sql.NullString
	err := transaction.WithContext(ctx).
		Raw(`SELECT type, sql FROM sqlite_schema WHERE name = ?`, object.Name).
		Row().Scan(&actualType, &actualSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"%w: read %s %q: %v",
			ErrContract,
			object.ObjectType,
			object.Name,
			err,
		)
	}
	if actualType != object.ObjectType || !actualSQL.Valid ||
		NormalizeSQL(actualSQL.String) != NormalizeSQL(CanonicalSQL(object.Statement)) {
		return false, fmt.Errorf(
			"%w: %s %q differs from canonical definition",
			ErrContract,
			object.ObjectType,
			object.Name,
		)
	}
	return true, nil
}

// CanonicalSQL 去除只影响创建幂等性的 IF NOT EXISTS。
func CanonicalSQL(statement string) string {
	return strings.Replace(statement, " IF NOT EXISTS", "", 1)
}

// NormalizeSQL 将 SQLite DDL 归一化为稳定的 checksum/contract 比较形式。
func NormalizeSQL(statement string) string {
	return strings.ToLower(strings.Join(strings.Fields(statement), " "))
}
