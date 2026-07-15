package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	libtnbsqlite "github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// WriteFunc performs one atomic transaction owned by the writer queue.
type WriteFunc func(context.Context, *gorm.DB) error

// ViewFunc performs queries during one lifecycle admission. All result sets and
// statements must be consumed or closed before the callback returns.
type ViewFunc func(context.Context, *gorm.DB) error

// WriteTx 是迁移期兼容别名；新代码直接使用 *gorm.DB。
type WriteTx = *gorm.DB

// ReadConn 是迁移期兼容别名；新代码直接使用 *gorm.DB。
type ReadConn = *gorm.DB

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

const maintenanceQueueCapacity = 1

// Store owns the process-local SQLite writer, read pool, and close lifecycle.
type Store struct {
	config    Config
	pragmas   PragmaSnapshot
	writerSQL *sql.DB
	readerSQL *sql.DB
	writer    *gorm.DB
	reader    *gorm.DB

	writeQueue       chan writeJob
	maintenanceQueue chan writeJob
	shutdown         chan struct{}
	done             chan struct{}
	closeOnce        sync.Once

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

	var writerSQL *sql.DB
	var readerSQL *sql.DB
	defer func() {
		if returnErr == nil {
			return
		}
		if readerSQL != nil {
			_ = readerSQL.Close()
		}
		if writerSQL != nil {
			_ = writerSQL.Close()
		}
	}()

	writerSQL, err = sql.Open(driverName, writerDSN(config))
	if err != nil {
		return nil, classifyError("open writer", err)
	}
	writerSQL.SetMaxOpenConns(1)
	writerSQL.SetMaxIdleConns(1)
	if err := writerSQL.PingContext(ctx); err != nil {
		return nil, classifyError("ping writer", err)
	}
	if err := secureDatabaseFile(config.Path, repairExistingPermissions || !databaseExisted); err != nil {
		return nil, err
	}
	writerPragmas, err := readPragmas(ctx, writerSQL)
	if err != nil {
		return nil, err
	}
	if err := validatePragmas("writer", writerPragmas, config, false); err != nil {
		return nil, err
	}

	readerSQL, err = sql.Open(driverName, readerDSN(config))
	if err != nil {
		return nil, classifyError("open reader", err)
	}
	readerSQL.SetMaxOpenConns(config.MaxReadConnections)
	readerSQL.SetMaxIdleConns(config.MaxReadConnections)
	if err := readerSQL.PingContext(ctx); err != nil {
		return nil, classifyError("ping reader", err)
	}
	readerPragmas, err := readPragmas(ctx, readerSQL)
	if err != nil {
		return nil, err
	}
	if err := validatePragmas("reader", readerPragmas, config, true); err != nil {
		return nil, err
	}

	writer, err := openGORM(writerSQL)
	if err != nil {
		return nil, classifyError("open GORM writer", err)
	}
	reader, err := openGORM(readerSQL)
	if err != nil {
		return nil, classifyError("open GORM reader", err)
	}

	store := &Store{
		config:           config,
		pragmas:          PragmaSnapshot{Writer: writerPragmas, Reader: readerPragmas},
		writerSQL:        writerSQL,
		readerSQL:        readerSQL,
		writer:           writer,
		reader:           reader,
		writeQueue:       make(chan writeJob, config.WriteQueueCapacity),
		maintenanceQueue: make(chan writeJob, maintenanceQueueCapacity),
		shutdown:         make(chan struct{}),
		done:             make(chan struct{}),
		state:            stateOpen,
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
	return store.enqueueWrite(ctx, write, store.writeQueue, "enqueue write")
}

// WriteMaintenance admits one transaction to a separate bounded queue that is
// only serviced when no normal write is waiting. It otherwise has the same
// authoritative commit, rollback, cancellation, and close semantics as Write.
func (store *Store) WriteMaintenance(ctx context.Context, write WriteFunc) error {
	return store.enqueueWrite(ctx, write, store.maintenanceQueue, "enqueue maintenance write")
}

func (store *Store) enqueueWrite(ctx context.Context, write WriteFunc, queue chan<- writeJob, operation string) error {
	if write == nil {
		return newClassifiedError(operation, ErrInvalidConfig, fmt.Errorf("write callback is nil"))
	}
	if err := ctx.Err(); err != nil {
		return classifyError(operation, err)
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
		admissionErr = newClassifiedError(operation, ErrClosing, nil)
	case stateClosed:
		admissionErr = newClassifiedError(operation, ErrClosed, nil)
	default:
		select {
		case <-ctx.Done():
			admissionErr = classifyError(operation, ctx.Err())
		case queue <- job:
		case <-store.shutdown:
			admissionErr = newClassifiedError(operation, ErrClosing, nil)
		default:
			admissionErr = newClassifiedError(operation, ErrQueueFull, nil)
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

	reader := store.reader.Session(&gorm.Session{NewDB: true, Context: ctx})
	err := view(ctx, reader)
	if contextErr := ctx.Err(); err != nil && contextErr != nil {
		err = errors.Join(contextErr, err)
	}
	return classifyError("view", err)
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
	var pendingMaintenance *writeJob
	for {
		if pendingMaintenance != nil {
			// Admission also holds stateMu. Keeping it across the empty check
			// establishes the exact point after which a new normal write waits
			// behind the maintenance transaction that is about to start.
			store.stateMu.Lock()
			select {
			case job := <-store.writeQueue:
				store.stateMu.Unlock()
				job.result <- store.executeWrite(job)
				continue
			default:
				job := *pendingMaintenance
				pendingMaintenance = nil
				store.stateMu.Unlock()
				job.result <- store.executeWrite(job)
				continue
			}
		}

		select {
		case job := <-store.writeQueue:
			job.result <- store.executeWrite(job)
			continue
		default:
		}

		select {
		case job := <-store.writeQueue:
			job.result <- store.executeWrite(job)
		case job := <-store.maintenanceQueue:
			pendingMaintenance = &job
		case <-store.shutdown:
			store.drainAndClose(pendingMaintenance)
			return
		}
	}
}

func (store *Store) drainAndClose(pendingMaintenance *writeJob) {
	for {
		select {
		case job := <-store.writeQueue:
			job.result <- store.executeWrite(job)
		default:
			if pendingMaintenance != nil {
				job := *pendingMaintenance
				pendingMaintenance = nil
				job.result <- store.executeWrite(job)
				continue
			}
			select {
			case job := <-store.maintenanceQueue:
				job.result <- store.executeWrite(job)
				continue
			default:
			}
			store.reads.Wait()
			closeErr := errors.Join(store.readerSQL.Close(), store.writerSQL.Close())
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

	transaction, err := store.writerSQL.BeginTx(job.ctx, nil)
	if err != nil {
		return classifyError("begin write", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = transaction.Rollback()
			returnErr = newClassifiedError("write callback", ErrCallbackPanic, fmt.Errorf("%v", recovered))
		}
	}()

	gormTransaction, err := openGORM(transaction)
	if err != nil {
		_ = transaction.Rollback()
		return classifyError("bind GORM transaction", err)
	}
	if err := job.write(job.ctx, gormTransaction.WithContext(job.ctx)); err != nil {
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

func openGORM(connection gorm.ConnPool) (*gorm.DB, error) {
	return gorm.Open(libtnbsqlite.New(libtnbsqlite.Config{Conn: connection}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
}
