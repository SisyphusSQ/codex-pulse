package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenUsesApplicationSupportPathAndPrivatePermissions(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Codex Pulse v0.1 only supports macOS")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	dataDir := filepath.Join(home, "Library", "Application Support", "Codex Pulse")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create loose data directory: %v", err)
	}
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatalf("set loose data directory permissions: %v", err)
	}

	store, err := Open(context.Background(), Config{})
	if err != nil {
		t.Fatalf("open default store: %v", err)
	}
	path := store.Config().Path
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("close default store: %v", err)
	}

	wantPath := filepath.Join(dataDir, "codex-pulse.db")
	if path != wantPath {
		t.Fatalf("default path = %q, want %q", path, wantPath)
	}

	dirInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat data directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("data directory mode = %#o, want 0700", got)
	}

	dbInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if got := dbInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("database mode = %#o, want 0600", got)
	}

	reopened, err := Open(context.Background(), Config{})
	if err != nil {
		t.Fatalf("reopen default store: %v", err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
}

func TestOpenRejectsSymlinkDataDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "data")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := Open(context.Background(), Config{Path: filepath.Join(link, "codex-pulse.db")})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Open() error = %v, want ErrInvalidPath", err)
	}
}

func TestOpenRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "negative busy timeout", mutate: func(c *Config) { c.BusyTimeout = -time.Second }},
		{name: "sub-millisecond busy timeout", mutate: func(c *Config) { c.BusyTimeout = 500 * time.Microsecond }},
		{name: "busy timeout exceeds sqlite integer", mutate: func(c *Config) { c.BusyTimeout = (time.Duration(math.MaxInt32) + 1) * time.Millisecond }},
		{name: "negative queue capacity", mutate: func(c *Config) { c.WriteQueueCapacity = -1 }},
		{name: "negative read connections", mutate: func(c *Config) { c.MaxReadConnections = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Path: filepath.Join(t.TempDir(), "codex-pulse.db")}
			tt.mutate(&config)
			_, err := Open(context.Background(), config)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Open() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestOpenDoesNotChangeExistingCustomPathPermissions(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		dataDirectory := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(dataDirectory, 0o755); err != nil {
			t.Fatalf("create custom directory: %v", err)
		}
		if err := os.Chmod(dataDirectory, 0o755); err != nil {
			t.Fatalf("set custom directory mode: %v", err)
		}
		databasePath := filepath.Join(dataDirectory, "codex-pulse.db")

		store, err := Open(context.Background(), Config{Path: databasePath})
		if store != nil {
			_ = store.Close(context.Background())
		}
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("Open() error = %v, want ErrPermission", err)
		}
		info, statErr := os.Stat(dataDirectory)
		if statErr != nil {
			t.Fatalf("stat custom directory: %v", statErr)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Fatalf("custom directory mode = %#o, want unchanged 0755", got)
		}
		if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
			t.Fatalf("database was created despite permission rejection: %v", statErr)
		}
	})

	t.Run("database file", func(t *testing.T) {
		dataDirectory := t.TempDir()
		databasePath := filepath.Join(dataDirectory, "codex-pulse.db")
		if err := os.WriteFile(databasePath, nil, 0o644); err != nil {
			t.Fatalf("create custom database: %v", err)
		}
		if err := os.Chmod(databasePath, 0o644); err != nil {
			t.Fatalf("set custom database mode: %v", err)
		}

		store, err := Open(context.Background(), Config{Path: databasePath})
		if store != nil {
			_ = store.Close(context.Background())
		}
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("Open() error = %v, want ErrPermission", err)
		}
		info, statErr := os.Stat(databasePath)
		if statErr != nil {
			t.Fatalf("stat custom database: %v", statErr)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("custom database mode = %#o, want unchanged 0644", got)
		}
	})
}

func TestOpenReadsBackRequiredPragmas(t *testing.T) {
	store := openTestStore(t, Config{
		BusyTimeout:        75 * time.Millisecond,
		MaxReadConnections: 3,
	})

	got := store.Pragmas()
	want := ConnectionPragmas{
		JournalMode:       "wal",
		ForeignKeys:       true,
		BusyTimeoutMillis: 75,
		Synchronous:       SynchronousNormal,
	}
	if got.Writer != want {
		t.Fatalf("writer pragmas = %+v, want %+v", got.Writer, want)
	}
	want.QueryOnly = true
	if got.Reader != want {
		t.Fatalf("reader pragmas = %+v, want %+v", got.Reader, want)
	}

	config := store.Config()
	if config.MaxReadConnections != 3 {
		t.Fatalf("MaxReadConnections = %d, want 3", config.MaxReadConnections)
	}
}

func TestStoreWriteIsFIFOAndRollsBackCallbackFailures(t *testing.T) {
	store := openTestStore(t, Config{WriteQueueCapacity: 2})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan error, 3)

	go func() {
		results <- store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "first"}).Error
		})
	}()
	<-firstStarted

	go func() {
		results <- insertEvent(store, context.Background(), "second")
	}()
	waitForQueueDepth(t, store, 1)
	go func() {
		results <- insertEvent(store, context.Background(), "third")
	}()
	waitForQueueDepth(t, store, 2)
	close(releaseFirst)

	for range 3 {
		if err := <-results; err != nil {
			t.Fatalf("queued write: %v", err)
		}
	}

	values := readEventValues(t, store)
	if got, want := strings.Join(values, ","), "first,second,third"; got != want {
		t.Fatalf("write order = %q, want %q", got, want)
	}

	errBoom := errors.New("callback failed")
	err := store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
		if execErr := tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "rollback"}).Error; execErr != nil {
			return execErr
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Write() error = %v, want callback error", err)
	}
	if values := readEventValues(t, store); len(values) != 3 {
		t.Fatalf("rows after rollback = %v, want original three rows", values)
	}

	err = store.Write(context.Background(), func(context.Context, WriteTx) error {
		panic("callback panic")
	})
	if !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("panic error = %v, want ErrCallbackPanic", err)
	}
	if err := insertEvent(store, context.Background(), "after-panic"); err != nil {
		t.Fatalf("write after callback panic: %v", err)
	}
}

func TestStoreReturnsQueueFullAndSkipsCanceledQueuedWrite(t *testing.T) {
	store := openTestStore(t, Config{WriteQueueCapacity: 1})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "first"}).Error
		})
	}()
	<-firstStarted

	queuedContext, cancelQueued := context.WithCancel(context.Background())
	queuedResult := make(chan error, 1)
	go func() {
		queuedResult <- insertEvent(store, queuedContext, "canceled")
	}()
	waitForQueueDepth(t, store, 1)

	err := insertEvent(store, context.Background(), "overflow")
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("overflow error = %v, want ErrQueueFull", err)
	}

	cancelQueued()
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := <-queuedResult; !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queued write error = %v, want ErrCanceled and context.Canceled", err)
	}

	values := readEventValues(t, store)
	if got, want := strings.Join(values, ","), "first"; got != want {
		t.Fatalf("persisted values = %q, want %q", got, want)
	}
}

func TestStorePrioritizesNormalWritesOverQueuedMaintenance(t *testing.T) {
	store := openTestStore(t, Config{WriteQueueCapacity: 2})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan error, 3)
	go func() {
		results <- store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "first"}).Error
		})
	}()
	<-firstStarted

	go func() {
		results <- store.WriteMaintenance(context.Background(), func(ctx context.Context, tx WriteTx) error {
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "maintenance"}).Error
		})
	}()
	waitForMaintenanceQueueDepth(t, store, 1)

	go func() {
		results <- insertEvent(store, context.Background(), "normal")
	}()
	waitForQueueDepth(t, store, 1)
	close(releaseFirst)

	for range 3 {
		if err := <-results; err != nil {
			t.Fatalf("queued write: %v", err)
		}
	}
	if got, want := strings.Join(readEventValues(t, store), ","), "first,normal,maintenance"; got != want {
		t.Fatalf("write order = %q, want %q", got, want)
	}
}

func TestStoreMaintenanceQueueIsBoundedAndSkipsCanceledWork(t *testing.T) {
	store := openTestStore(t, Config{})
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

	queuedContext, cancelQueued := context.WithCancel(context.Background())
	queuedResult := make(chan error, 1)
	go func() {
		queuedResult <- store.WriteMaintenance(queuedContext, func(ctx context.Context, tx WriteTx) error {
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "canceled"}).Error
		})
	}()
	waitForMaintenanceQueueDepth(t, store, 1)

	err := store.WriteMaintenance(context.Background(), func(context.Context, WriteTx) error { return nil })
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("maintenance overflow error = %v, want ErrQueueFull", err)
	}

	cancelQueued()
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := <-queuedResult; !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled maintenance error = %v, want ErrCanceled and context.Canceled", err)
	}
	if values := readEventValues(t, store); len(values) != 0 {
		t.Fatalf("canceled maintenance persisted values: %v", values)
	}
}

func TestStoreWriteWaitsForAuthoritativeResultAfterAdmission(t *testing.T) {
	store := openTestStore(t, Config{})
	createEventsTable(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- store.Write(ctx, func(_ context.Context, tx WriteTx) error {
			close(callbackStarted)
			<-releaseCallback
			return tx.WithContext(context.Background()).Table("events").Create(map[string]any{"value": "must-rollback"}).Error
		})
	}()
	<-callbackStarted
	cancel()

	select {
	case err := <-result:
		close(releaseCallback)
		t.Fatalf("Write returned before the claimed worker result: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCallback)

	err := <-result
	if !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() error = %v, want authoritative ErrCanceled and context.Canceled", err)
	}
	if values := readEventValues(t, store); len(values) != 0 {
		t.Fatalf("canceled claimed write persisted values: %v", values)
	}
}

func TestCallbackSessionsAreBoundToQueueOwnedPools(t *testing.T) {
	store := openTestStore(t, Config{})

	err := store.Write(context.Background(), func(_ context.Context, tx WriteTx) error {
		if tx == store.writer {
			return errors.New("write callback received the shared root GORM session")
		}
		if tx.Statement == nil || tx.Statement.ConnPool == store.writerSQL {
			return errors.New("write callback is not bound to the queue-owned transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write callback binding: %v", err)
	}

	err = store.View(context.Background(), func(_ context.Context, reader ReadConn) error {
		if reader == store.reader {
			return errors.New("read callback received the shared root GORM session")
		}
		if reader.Statement == nil || reader.Statement.ConnPool != store.readerSQL {
			return errors.New("read callback is not bound to the read-only pool")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read callback binding: %v", err)
	}
}

func TestStoreSupportsConcurrentWALReadsAndQueuedWrites(t *testing.T) {
	store := openTestStore(t, Config{
		WriteQueueCapacity: 64,
		MaxReadConnections: 8,
	})
	createEventsTable(t, store)

	const writers = 40
	const readers = 8
	start := make(chan struct{})
	errorsFound := make(chan error, writers+readers)
	var wait sync.WaitGroup

	for index := range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if err := insertEvent(store, context.Background(), fmt.Sprintf("write-%02d", index)); err != nil {
				errorsFound <- err
			}
		}()
	}
	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 20 {
				err := store.View(context.Background(), func(ctx context.Context, db ReadConn) error {
					var count int64
					return db.WithContext(ctx).Table("events").Count(&count).Error
				})
				if err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}

	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent operation: %v", err)
	}

	if got := len(readEventValues(t, store)); got != writers {
		t.Fatalf("row count = %d, want %d", got, writers)
	}
}

func TestStoreViewEnforcesReadOnlyConnection(t *testing.T) {
	store := openTestStore(t, Config{})
	createEventsTable(t, store)

	err := store.View(context.Background(), func(ctx context.Context, db ReadConn) error {
		return db.WithContext(ctx).Table("events").Create(map[string]any{"value": "forbidden"}).Error
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("View write error = %v, want ErrReadOnly", err)
	}
	if got := len(readEventValues(t, store)); got != 0 {
		t.Fatalf("row count = %d, want 0", got)
	}
}

func TestStoreClassifiesExternalWriterContentionAsBusy(t *testing.T) {
	store := openTestStore(t, Config{BusyTimeout: 20 * time.Millisecond})
	createEventsTable(t, store)

	locker, err := sql.Open(driverName, writerDSN(store.Config()))
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	locker.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = locker.Close() })

	connection, err := locker.Conn(context.Background())
	if err != nil {
		t.Fatalf("acquire lock connection: %v", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("begin external write transaction: %v", err)
	}

	err = insertEvent(store, context.Background(), "blocked")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("contended write error = %v, want ErrBusy", err)
	}

	if _, err := connection.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatalf("rollback external write transaction: %v", err)
	}
	if err := insertEvent(store, context.Background(), "after-lock"); err != nil {
		t.Fatalf("write after releasing lock: %v", err)
	}
}

func TestStoreCloseDrainsAcceptedWritesRejectsNewWorkAndIsIdempotent(t *testing.T) {
	store := openTestStoreWithoutCleanup(t, Config{WriteQueueCapacity: 1})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "first"}).Error
		})
	}()
	<-firstStarted

	secondResult := make(chan error, 1)
	go func() {
		secondResult <- insertEvent(store, context.Background(), "second")
	}()
	waitForQueueDepth(t, store, 1)

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- store.Close(context.Background())
	}()
	waitForState(t, store, stateClosing)

	if err := insertEvent(store, context.Background(), "rejected"); !errors.Is(err, ErrClosing) {
		t.Fatalf("write during close error = %v, want ErrClosing", err)
	}
	if err := store.View(context.Background(), func(context.Context, ReadConn) error { return nil }); !errors.Is(err, ErrClosing) {
		t.Fatalf("view during close error = %v, want ErrClosing", err)
	}

	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := <-secondResult; err != nil {
		t.Fatalf("second write: %v", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := insertEvent(store, context.Background(), "closed"); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close error = %v, want ErrClosed", err)
	}
	for range 8 {
		if err := store.Close(context.Background()); err != nil {
			t.Fatalf("idempotent close: %v", err)
		}
	}

	reopened, err := Open(context.Background(), Config{Path: store.Config().Path})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	values := readEventValues(t, reopened)
	if got, want := strings.Join(values, ","), "first,second"; got != want {
		t.Fatalf("drained values = %q, want %q", got, want)
	}
}

func TestStoreCloseDrainsAcceptedMaintenanceAndRejectsNewMaintenance(t *testing.T) {
	store := openTestStoreWithoutCleanup(t, Config{})
	createEventsTable(t, store)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
			close(firstStarted)
			<-releaseFirst
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "first"}).Error
		})
	}()
	<-firstStarted

	maintenanceResult := make(chan error, 1)
	go func() {
		maintenanceResult <- store.WriteMaintenance(context.Background(), func(ctx context.Context, tx WriteTx) error {
			return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": "maintenance"}).Error
		})
	}()
	waitForMaintenanceQueueDepth(t, store, 1)

	closeResult := make(chan error, 1)
	go func() { closeResult <- store.Close(context.Background()) }()
	waitForState(t, store, stateClosing)

	if err := store.WriteMaintenance(context.Background(), func(context.Context, WriteTx) error { return nil }); !errors.Is(err, ErrClosing) {
		t.Fatalf("maintenance during close error = %v, want ErrClosing", err)
	}
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := <-maintenanceResult; err != nil {
		t.Fatalf("maintenance write: %v", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := store.WriteMaintenance(context.Background(), func(context.Context, WriteTx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("maintenance after close error = %v, want ErrClosed", err)
	}

	reopened, err := Open(context.Background(), Config{Path: store.Config().Path})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	if got, want := strings.Join(readEventValues(t, reopened), ","), "first,maintenance"; got != want {
		t.Fatalf("drained values = %q, want %q", got, want)
	}
}

func TestStoreCloseWaitsForInflightReadAndCanceledWaitDoesNotAbortClose(t *testing.T) {
	store := openTestStoreWithoutCleanup(t, Config{})
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	readResult := make(chan error, 1)
	go func() {
		readResult <- store.View(context.Background(), func(context.Context, ReadConn) error {
			close(readStarted)
			<-releaseRead
			return nil
		})
	}()
	<-readStarted

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Close(canceled); !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled close wait error = %v, want ErrCanceled and context.Canceled", err)
	}
	waitForState(t, store, stateClosing)

	closed := make(chan error, 1)
	go func() { closed <- store.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Close completed before inflight read: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseRead)
	if err := <-readResult; err != nil {
		t.Fatalf("inflight read: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("close after read: %v", err)
	}
}

func openTestStore(t *testing.T, config Config) *Store {
	t.Helper()
	store := openTestStoreWithoutCleanup(t, config)
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	return store
}

func openTestStoreWithoutCleanup(t *testing.T, config Config) *Store {
	t.Helper()
	if config.Path == "" {
		config.Path = filepath.Join(t.TempDir(), "store-data", "codex-pulse.db")
	}
	store, err := Open(context.Background(), config)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	return store
}

func createEventsTable(t *testing.T, store *Store) {
	t.Helper()
	err := store.Write(context.Background(), func(ctx context.Context, tx WriteTx) error {
		return tx.WithContext(ctx).Exec(`
			CREATE TABLE IF NOT EXISTS events (
				sequence INTEGER PRIMARY KEY AUTOINCREMENT,
				value TEXT NOT NULL
			) STRICT
		`).Error
	})
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}
}

func insertEvent(store *Store, ctx context.Context, value string) error {
	return store.Write(ctx, func(ctx context.Context, tx WriteTx) error {
		return tx.WithContext(ctx).Table("events").Create(map[string]any{"value": value}).Error
	})
}

func readEventValues(t *testing.T, store *Store) []string {
	t.Helper()
	var values []string
	err := store.View(context.Background(), func(ctx context.Context, db ReadConn) error {
		return db.WithContext(ctx).Table("events").Order("sequence").Pluck("value", &values).Error
	})
	if err != nil {
		t.Fatalf("read event values: %v", err)
	}
	return values
}

func waitForQueueDepth(t *testing.T, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.writeQueue) == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("write queue depth = %d, want %d", len(store.writeQueue), want)
}

func waitForMaintenanceQueueDepth(t *testing.T, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.maintenanceQueue) == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("maintenance queue depth = %d, want %d", len(store.maintenanceQueue), want)
}

func waitForState(t *testing.T, store *Store, want lifecycleState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.stateMu.Lock()
		got := store.state
		store.stateMu.Unlock()
		if got == want {
			return
		}
		runtime.Gosched()
	}
	store.stateMu.Lock()
	got := store.state
	store.stateMu.Unlock()
	t.Fatalf("store state = %v, want %v", got, want)
}
