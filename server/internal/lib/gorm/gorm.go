package gormv2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	gormSQLite "github.com/libtnb/sqlite"
	"go.uber.org/fx"
	gormMySQL "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/health"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
)

type Engine struct{ gorm *gorm.DB }

func New(lifecycle fx.Lifecycle, c config.Config) (*Engine, error) {
	conf, err := normalizeConfig(c.Database)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(conf.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("load database timezone: %w", err)
	}
	engine := &Engine{}
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			var dialector gorm.Dialector
			if conf.Driver == "sqlite" {
				path, err := filepath.Abs(conf.Path)
				if err != nil {
					return fmt.Errorf("resolve sqlite path: %w", err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					return fmt.Errorf("create sqlite directory: %w", err)
				}
				stat, err := os.Stat(filepath.Dir(path))
				if err != nil || stat.Mode().Perm()&0077 != 0 {
					return fmt.Errorf("sqlite directory must be private (0700)")
				}
				file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					return fmt.Errorf("open sqlite file: %w", err)
				}
				if err := file.Close(); err != nil {
					return err
				}
				stat, err = os.Stat(path)
				if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
					return fmt.Errorf("sqlite file must be private (0600)")
				}
				u := url.URL{Scheme: "file", Path: path}
				q := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"}}
				u.RawQuery = q.Encode()
				dialector = gormSQLite.Open(u.String())
			} else {
				d := mysqlDriver.NewConfig()
				d.User = conf.User
				d.Passwd = conf.Password
				d.Net = "tcp"
				d.Addr = net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port))
				d.DBName = conf.Database
				d.ParseTime = true
				d.ClientFoundRows = true
				d.Loc = location
				d.Params = map[string]string{"charset": conf.Charset}
				d.Timeout = c.ContextTimeout
				d.ReadTimeout = c.ContextTimeout
				d.WriteTimeout = c.ContextTimeout
				d.TLSConfig = conf.TLS
				dialector = gormMySQL.New(gormMySQL.Config{DSN: d.FormatDSN(), SkipInitializeWithVersion: true})
			}
			db, err := gorm.Open(dialector, &gorm.Config{DisableAutomaticPing: true, TranslateError: true, Logger: logger.Discard})
			if err != nil {
				return fmt.Errorf("open %s database: %w", conf.Driver, err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				return fmt.Errorf("get sql database: %w", err)
			}
			sqlDB.SetConnMaxLifetime(conf.ConnMaxLifetime)
			sqlDB.SetMaxIdleConns(conf.MaxIdleConns)
			sqlDB.SetMaxOpenConns(conf.MaxOpenConns)
			if conf.Driver == "sqlite" {
				// SQLite 的单一连接串行化本中心事务，与本机数据库完全隔离。
				sqlDB.SetMaxOpenConns(1)
				sqlDB.SetMaxIdleConns(1)
			}
			if err := sqlDB.PingContext(ctx); err != nil {
				_ = sqlDB.Close()
				return fmt.Errorf("ping %s database: %w", conf.Driver, err)
			}
			engine.gorm = db
			engine.wrapLog()
			return nil
		},
		OnStop: func(context.Context) error {
			if engine.gorm == nil {
				return nil
			}
			db, err := engine.gorm.DB()
			if err != nil {
				return err
			}
			return db.Close()
		},
	})
	return engine, nil
}
func Readiness(engine *Engine) health.Check {
	return health.Check{Name: "database", Enabled: true, Ping: func(ctx context.Context) error {
		if engine.gorm == nil {
			return errors.New("database not started")
		}
		db, err := engine.gorm.DB()
		if err != nil {
			return err
		}
		return db.PingContext(ctx)
	}}
}
func (e *Engine) Connect() *gorm.DB { return e.gorm }
func (e *Engine) SetLogMode(enabled bool) {
	if !enabled {
		e.gorm.Logger = e.gorm.Logger.LogMode(LogLevelSilent)
	}
}
func (e *Engine) SetLogLevel(level LogLevel) { e.gorm.Logger = e.gorm.Logger.LogMode(level) }
func (e *Engine) wrapLog() {
	if log.Logger == nil {
		return
	}
	e.gorm.Logger = queryLogger{level: logger.Warn}
}

type transactionKey struct{}
type transactionContext struct {
	engine *Engine
	db     *gorm.DB
}

// DB 在事务回调中复用同一个连接；repository 必须使用此入口。
func (e *Engine) DB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(transactionKey{}).(transactionContext); ok {
		if tx.engine == e {
			return tx.db.WithContext(ctx)
		}
		db := e.gorm.WithContext(ctx)
		_ = db.AddError(errors.New("cross-database transaction context is unsupported"))
		return db
	}
	return e.gorm.WithContext(ctx)
}

// Transaction 只支持本数据库事务，不支持嵌套或跨数据库事务。
func (e *Engine) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return e.transaction(ctx, fn, nil)
}

// ReadSnapshot 不依赖 MySQL 的部署默认隔离级别；SQLite 的同一读事务保留快照。
func (e *Engine) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	var options *sql.TxOptions
	if e.gorm != nil && e.gorm.Dialector.Name() == "mysql" {
		options = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	return e.transaction(ctx, fn, options)
}
func (e *Engine) transaction(ctx context.Context, fn func(context.Context) error, options *sql.TxOptions) error {
	if ctx.Value(transactionKey{}) != nil {
		return errors.New("nested or cross-database transaction is unsupported")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.gorm == nil {
		return errors.New("database not started")
	}
	return e.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fn(context.WithValue(ctx, transactionKey{}, transactionContext{engine: e, db: tx})); err != nil {
			return err
		}
		return ctx.Err()
	}, options)
}
