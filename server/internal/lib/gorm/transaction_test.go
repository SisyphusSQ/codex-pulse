package gormv2

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestTransactionSharesConnectionAndCommits(t *testing.T) {
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
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO first_record").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO second_record").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()
	err = engine.Transaction(t.Context(), func(ctx context.Context) error {
		if err := engine.DB(ctx).Exec("INSERT INTO first_record VALUES (?)", 1).Error; err != nil {
			return err
		}
		return engine.DB(ctx).Exec("INSERT INTO second_record VALUES (?)", 2).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionRollsBackBusinessFailure(t *testing.T) {
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
	failure := errors.New("second write failed")
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO first_record").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()
	err = engine.Transaction(t.Context(), func(ctx context.Context) error {
		if err := engine.DB(ctx).Exec("INSERT INTO first_record VALUES (?)", 1).Error; err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("business error lost: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledTransactionDoesNotBegin(t *testing.T) {
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
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = engine.Transaction(ctx, func(context.Context) error { t.Fatal("canceled callback executed"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionRejectsForeignEngineContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	first := &Engine{gorm: gdb}
	second := &Engine{gorm: gdb}
	mock.ExpectBegin()
	mock.ExpectRollback()
	err = first.Transaction(t.Context(), func(ctx context.Context) error {
		result := second.DB(ctx).Exec("INSERT INTO foreign_record VALUES (?)", 1).Error
		if result == nil || result.Error() != "cross-database transaction context is unsupported" {
			t.Errorf("foreign connection was not rejected: %v", result)
		}
		return result
	})
	if err == nil {
		t.Fatal("foreign transaction succeeded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotUsesOneConnection(t *testing.T) {
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
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mock.ExpectCommit()
	err = engine.ReadSnapshot(t.Context(), func(ctx context.Context) error {
		var count int64
		err := engine.DB(ctx).Raw("SELECT count FROM snapshot_record").Scan(&count).Error
		if count != 7 {
			t.Fatal("snapshot result lost")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
