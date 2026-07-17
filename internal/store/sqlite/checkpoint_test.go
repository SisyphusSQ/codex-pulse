package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCheckpointWALRunsFixedPassiveCheckpoint(t *testing.T) {
	store := openTestStore(t, Config{})
	createEventsTable(t, store)
	for index := range 32 {
		if err := insertEvent(store, context.Background(), fmt.Sprintf("event-%02d", index)); err != nil {
			t.Fatalf("insert event %d: %v", index, err)
		}
	}

	report, err := store.CheckpointWAL(context.Background())
	if err != nil {
		t.Fatalf("CheckpointWAL() error = %v", err)
	}
	if report.LogFrames < 0 || report.CheckpointedFrames < 0 || report.CheckpointedFrames > report.LogFrames {
		t.Fatalf("CheckpointWAL() report = %#v, want valid SQLite frame counts", report)
	}
	if report.Busy {
		t.Fatalf("CheckpointWAL() report = %#v, want no busy writer", report)
	}
}

func TestCheckpointWALDoesNotBlockActiveReadSnapshot(t *testing.T) {
	store := openTestStore(t, Config{})
	createEventsTable(t, store)
	if err := insertEvent(store, context.Background(), "before-reader"); err != nil {
		t.Fatalf("insert initial event: %v", err)
	}

	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	readResult := make(chan error, 1)
	go func() {
		readResult <- store.View(context.Background(), func(ctx context.Context, connection ReadConn) error {
			transaction := connection.WithContext(ctx).Begin()
			if transaction.Error != nil {
				return transaction.Error
			}
			defer transaction.Rollback()
			var count int64
			if err := transaction.Table("events").Count(&count).Error; err != nil {
				return err
			}
			close(readStarted)
			<-releaseRead
			return nil
		})
	}()
	<-readStarted
	if err := insertEvent(store, context.Background(), "while-reader"); err != nil {
		t.Fatalf("insert during active reader: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report, err := store.CheckpointWAL(ctx)
	if err != nil {
		close(releaseRead)
		t.Fatalf("CheckpointWAL() with active reader error = %v", err)
	}
	if report.LogFrames < 1 || report.CheckpointedFrames < 0 || report.CheckpointedFrames > report.LogFrames {
		close(releaseRead)
		t.Fatalf("CheckpointWAL() report = %#v, want valid partial-or-complete PASSIVE result", report)
	}
	close(releaseRead)
	if err := <-readResult; err != nil {
		t.Fatalf("active read result: %v", err)
	}
}

func TestCheckpointWALUsesLowPriorityQueueAndSkipsCanceledWork(t *testing.T) {
	store := openTestStore(t, Config{WriteQueueCapacity: 2})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- store.Write(context.Background(), func(context.Context, WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		})
	}()
	<-firstStarted

	checkpointContext, cancelCheckpoint := context.WithCancel(context.Background())
	checkpointResult := make(chan error, 1)
	go func() {
		_, err := store.CheckpointWAL(checkpointContext)
		checkpointResult <- err
	}()
	waitForMaintenanceQueueDepth(t, store, 1)

	normalResult := make(chan error, 1)
	go func() { normalResult <- insertEvent(store, context.Background(), "normal") }()
	waitForQueueDepth(t, store, 1)
	cancelCheckpoint()
	close(releaseFirst)

	if err := <-firstResult; err != nil {
		t.Fatalf("first write: %v", err)
	}
	select {
	case err := <-normalResult:
		if err != nil {
			t.Fatalf("normal write: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("normal write did not complete")
	}
	if err := <-checkpointResult; !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled checkpoint error = %v, want ErrCanceled and context.Canceled", err)
	}
	if values := readEventValues(t, store); len(values) != 1 || values[0] != "normal" {
		t.Fatalf("persisted values = %v, want prioritized normal write", values)
	}
}

func TestCheckpointWALRejectsWorkAfterClose(t *testing.T) {
	store := openTestStoreWithoutCleanup(t, Config{})
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := store.CheckpointWAL(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("CheckpointWAL() error = %v, want ErrClosed", err)
	}
}
