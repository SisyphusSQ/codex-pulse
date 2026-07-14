package sqlite

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type callbackRecord struct {
	ID    int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Value string `gorm:"column:value;not null"`
}

func (callbackRecord) TableName() string {
	return "callback_records"
}

// 测试 Store 在写队列和只读连接池中提供绑定当前 context 的 GORM 会话。
func TestStoreCallbacksProvideGORMTransactions(t *testing.T) {
	store := openTestStore(t, Config{})

	err := store.Write(context.Background(), func(ctx context.Context, tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		if err := tx.Migrator().CreateTable(&callbackRecord{}); err != nil {
			return err
		}
		return tx.Create(&callbackRecord{Value: "committed"}).Error
	})
	if err != nil {
		t.Fatalf("GORM write: %v", err)
	}

	var records []callbackRecord
	err = store.View(context.Background(), func(ctx context.Context, reader *gorm.DB) error {
		return reader.WithContext(ctx).Order("id").Find(&records).Error
	})
	if err != nil {
		t.Fatalf("GORM view: %v", err)
	}
	if len(records) != 1 || records[0].Value != "committed" {
		t.Fatalf("records = %+v, want one committed row", records)
	}

	errBoom := errors.New("rollback callback")
	err = store.Write(context.Background(), func(ctx context.Context, tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(&callbackRecord{Value: "rolled-back"}).Error; err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("rollback error = %v, want callback error", err)
	}

	records = nil
	err = store.View(context.Background(), func(ctx context.Context, reader *gorm.DB) error {
		return reader.WithContext(ctx).Order("id").Find(&records).Error
	})
	if err != nil {
		t.Fatalf("GORM view after rollback: %v", err)
	}
	if len(records) != 1 || records[0].Value != "committed" {
		t.Fatalf("records after rollback = %+v, want only committed row", records)
	}
}
