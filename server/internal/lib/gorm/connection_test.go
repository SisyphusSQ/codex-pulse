package gormv2

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestConnectionSharesSinglePoolSlotWithTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{gorm: gdb}
	mock.ExpectExec("CREATE TABLE migration_probe").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO migration_probe").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	err = engine.Connection(t.Context(), func(ctx context.Context) error {
		if _, ok := engine.DB(ctx).Statement.ConnPool.(*sql.Conn); !ok {
			t.Fatal("connection not pinned")
		}
		if err := engine.DB(ctx).Exec("CREATE TABLE migration_probe(id int)").Error; err != nil {
			return err
		}
		return engine.Transaction(ctx, func(ctx context.Context) error {
			return engine.DB(ctx).Exec("INSERT INTO migration_probe VALUES (?)", 1).Error
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.Stats().InUse != 0 {
		t.Fatal("connection not returned")
	}
	if _, ok := engine.DB(t.Context()).Statement.ConnPool.(*sql.DB); !ok {
		t.Fatal("connection context leaked")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionDiscardsFailedSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{gorm: gdb}
	failure := errors.New("migration session failed")
	mock.ExpectClose()
	err = engine.Connection(t.Context(), func(context.Context) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatal("failure hidden", err)
	}
	if db.Stats().InUse != 0 {
		t.Fatal("failed session retained")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionRejectsForeignEngineTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	first, second := &Engine{gorm: gdb}, &Engine{gorm: gdb}
	mock.ExpectClose()
	err = first.Connection(t.Context(), func(ctx context.Context) error {
		return second.Transaction(ctx, func(context.Context) error { t.Fatal("foreign transaction began"); return nil })
	})
	if err == nil {
		t.Fatal("foreign connection accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
