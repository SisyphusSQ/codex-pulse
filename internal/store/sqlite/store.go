package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// WriteTx exposes SQL operations inside a queue-owned transaction without
// exposing Commit or Rollback to the callback.
type WriteTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// ReadConn exposes query-only operations backed by the read-only connection pool.
type ReadConn interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// WriteFunc performs one atomic transaction owned by the writer queue.
type WriteFunc func(context.Context, WriteTx) error

// ViewFunc performs queries during one lifecycle admission. All result sets and
// statements must be consumed or closed before the callback returns.
type ViewFunc func(context.Context, ReadConn) error

type writeTxView struct {
	transaction *sql.Tx
}

func (view writeTxView) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return view.transaction.ExecContext(ctx, query, args...)
}

func (view writeTxView) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return view.transaction.QueryContext(ctx, query, args...)
}

func (view writeTxView) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return view.transaction.QueryRowContext(ctx, query, args...)
}

func (view writeTxView) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return view.transaction.PrepareContext(ctx, query)
}

type readConnView struct {
	database *sql.DB
}

func (view readConnView) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return view.database.QueryContext(ctx, query, args...)
}

func (view readConnView) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return view.database.QueryRowContext(ctx, query, args...)
}

func (view readConnView) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return view.database.PrepareContext(ctx, query)
}

type lifecycleState uint8

const (
	stateOpen lifecycleState = iota
	stateClosing
	stateClosed
)

type writeJob struct {
	ctx    context.Context
	write  WriteFunc
	result chan error
}

// Store owns the process-local SQLite writer, read pool, and close lifecycle.
type Store struct {
	config  Config
	pragmas PragmaSnapshot
	writer  *sql.DB
	reader  *sql.DB

	writeQueue chan writeJob
	shutdown   chan struct{}
	done       chan struct{}
	closeOnce  sync.Once

	stateMu  sync.Mutex
	state    lifecycleState
	reads    sync.WaitGroup
	closeErr error
}

// Open initializes and validates a local SQLite store before starting its writer.
func Open(ctx context.Context, input Config) (_ *Store, returnErr error) {
	if err := ctx.Err(); err != nil {
		return nil, classifyError("open", err)
	}
	config, err := normalizeConfig(input)
	if err != nil {
		return nil, err
	}
	repairExistingPermissions := input.Path == ""
	databaseExisted, err := prepareDatabasePath(config.Path, repairExistingPermissions)
	if err != nil {
		return nil, err
	}

	var writer *sql.DB
	var reader *sql.DB
	defer func() {
		if returnErr == nil {
			return
		}
		if reader != nil {
			_ = reader.Close()
		}
		if writer != nil {
			_ = writer.Close()
		}
	}()

	writer, err = sql.Open(driverName, writerDSN(config))
	if err != nil {
		return nil, classifyError("open writer", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	if err := writer.PingContext(ctx); err != nil {
		return nil, classifyError("ping writer", err)
	}
	if err := secureDatabaseFile(config.Path, repairExistingPermissions || !databaseExisted); err != nil {
		return nil, err
	}
	writerPragmas, err := readPragmas(ctx, writer)
	if err != nil {
		return nil, err
	}
	if err := validatePragmas("writer", writerPragmas, config, false); err != nil {
		return nil, err
	}

	reader, err = sql.Open(driverName, readerDSN(config))
	if err != nil {
		return nil, classifyError("open reader", err)
	}
	reader.SetMaxOpenConns(config.MaxReadConnections)
	reader.SetMaxIdleConns(config.MaxReadConnections)
	if err := reader.PingContext(ctx); err != nil {
		return nil, classifyError("ping reader", err)
	}
	readerPragmas, err := readPragmas(ctx, reader)
	if err != nil {
		return nil, err
	}
	if err := validatePragmas("reader", readerPragmas, config, true); err != nil {
		return nil, err
	}

	store := &Store{
		config:     config,
		pragmas:    PragmaSnapshot{Writer: writerPragmas, Reader: readerPragmas},
		writer:     writer,
		reader:     reader,
		writeQueue: make(chan writeJob, config.WriteQueueCapacity),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		state:      stateOpen,
	}
	go store.runWriter()

	return store, nil
}

// Config returns the resolved immutable configuration used by the store.
func (store *Store) Config() Config {
	return store.config
}

// Pragmas returns the startup readback from the writer and reader connections.
func (store *Store) Pragmas() PragmaSnapshot {
	return store.pragmas
}

// Write admits one transaction to the bounded FIFO writer queue. Once admitted,
// it waits for the worker's authoritative commit or rollback result even when
// the context is canceled.
func (store *Store) Write(ctx context.Context, write WriteFunc) error {
	if write == nil {
		return newClassifiedError("write", ErrInvalidConfig, fmt.Errorf("write callback is nil"))
	}
	if err := ctx.Err(); err != nil {
		return classifyError("enqueue write", err)
	}

	job := writeJob{
		ctx:    ctx,
		write:  write,
		result: make(chan error, 1),
	}
	store.stateMu.Lock()
	var admissionErr error
	switch store.state {
	case stateClosing:
		admissionErr = newClassifiedError("enqueue write", ErrClosing, nil)
	case stateClosed:
		admissionErr = newClassifiedError("enqueue write", ErrClosed, nil)
	default:
		select {
		case <-ctx.Done():
			admissionErr = classifyError("enqueue write", ctx.Err())
		case store.writeQueue <- job:
		case <-store.shutdown:
			admissionErr = newClassifiedError("enqueue write", ErrClosing, nil)
		default:
			admissionErr = newClassifiedError("enqueue write", ErrQueueFull, nil)
		}
	}
	store.stateMu.Unlock()
	if admissionErr != nil {
		return admissionErr
	}

	return <-job.result
}

// View runs a callback against the read-only pool while the store is open.
func (store *Store) View(ctx context.Context, view ViewFunc) error {
	if view == nil {
		return newClassifiedError("view", ErrInvalidConfig, fmt.Errorf("view callback is nil"))
	}
	if err := ctx.Err(); err != nil {
		return classifyError("view", err)
	}

	store.stateMu.Lock()
	switch store.state {
	case stateClosing:
		store.stateMu.Unlock()
		return newClassifiedError("view", ErrClosing, nil)
	case stateClosed:
		store.stateMu.Unlock()
		return newClassifiedError("view", ErrClosed, nil)
	default:
		store.reads.Add(1)
		store.stateMu.Unlock()
	}
	defer store.reads.Done()

	return classifyError("view", view(ctx, readConnView{database: store.reader}))
}

// Close rejects new work, drains admitted writes, waits for reads, and closes
// both pools. A canceled wait does not abort the close already in progress.
func (store *Store) Close(ctx context.Context) error {
	store.closeOnce.Do(func() {
		store.stateMu.Lock()
		store.state = stateClosing
		close(store.shutdown)
		store.stateMu.Unlock()
	})

	select {
	case <-store.done:
		store.stateMu.Lock()
		err := store.closeErr
		store.stateMu.Unlock()
		return err
	case <-ctx.Done():
		return classifyError("wait for close", ctx.Err())
	}
}

func (store *Store) runWriter() {
	for {
		select {
		case job := <-store.writeQueue:
			job.result <- store.executeWrite(job)
		case <-store.shutdown:
			store.drainAndClose()
			return
		}
	}
}

func (store *Store) drainAndClose() {
	for {
		select {
		case job := <-store.writeQueue:
			job.result <- store.executeWrite(job)
		default:
			store.reads.Wait()
			closeErr := errors.Join(store.reader.Close(), store.writer.Close())
			store.stateMu.Lock()
			store.closeErr = classifyError("close connections", closeErr)
			store.state = stateClosed
			store.stateMu.Unlock()
			close(store.done)
			return
		}
	}
}

func (store *Store) executeWrite(job writeJob) (returnErr error) {
	if err := job.ctx.Err(); err != nil {
		return classifyError("begin write", err)
	}

	transaction, err := store.writer.BeginTx(job.ctx, nil)
	if err != nil {
		return classifyError("begin write", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = transaction.Rollback()
			returnErr = newClassifiedError("write callback", ErrCallbackPanic, fmt.Errorf("%v", recovered))
		}
	}()

	if err := job.write(job.ctx, writeTxView{transaction: transaction}); err != nil {
		if contextErr := job.ctx.Err(); contextErr != nil {
			err = errors.Join(contextErr, err)
		}
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return classifyError("write callback", err)
	}
	if err := job.ctx.Err(); err != nil {
		_ = transaction.Rollback()
		return classifyError("commit write", err)
	}
	if err := transaction.Commit(); err != nil {
		if contextErr := job.ctx.Err(); contextErr != nil {
			return classifyError("commit write", contextErr)
		}
		return classifyError("commit write", err)
	}
	return nil
}
