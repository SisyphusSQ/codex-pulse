package log

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/requestinfo"
	"github.com/SisyphusSQ/codex-pulse/server/utils/timeutil"
)

var (
	Logger *ZapLogger

	fileLogger *lumberjack.Logger
)

func New(config config.Config) error {
	c := config.Log
	if err := preCheck(c.LogLevel); err != nil {
		return err
	}
	var sinks []zapcore.WriteSyncer
	if c.Output == "stdout" || c.Output == "both" {
		sinks = append(sinks, zapcore.Lock(os.Stdout))
	}
	fileLogger = nil
	if c.Output == "file" || c.Output == "both" {
		if c.FileName == "" {
			return fmt.Errorf("log.fileName is required")
		}
		if dir := filepath.Dir(c.FileName); dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("create log directory: %w", err)
			}
		}
		fileLogger = &lumberjack.Logger{Filename: c.FileName, MaxSize: c.MaxSizeMb, MaxBackups: c.MaxBackupCount, MaxAge: c.MaxKeepDays, Compress: true}
		sinks = append(sinks, zapcore.AddSync(fileLogger))
	}
	if len(sinks) == 0 {
		return fmt.Errorf("invalid log output")
	}
	writeSyncer := zapcore.NewMultiWriteSyncer(sinks...)
	timeEncoder := zapcore.TimeEncoderOfLayout(timeutil.CSTLayout)
	cfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    customLevelEncoder,
		EncodeTime:     timeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   customCallerEncoder,
	}
	encoder := zapcore.NewJSONEncoder(cfg)
	core := zapcore.NewCore(encoder, writeSyncer, c.LogLevel)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)).Sugar()
	Logger = NewZapLogger(logger)

	return nil
}

func preCheck(logLevel zapcore.Level) error {
	if logLevel < zapcore.DebugLevel || logLevel > zapcore.FatalLevel {
		return fmt.Errorf("invalid log level %d: must be between -1 and 5", logLevel)
	}
	return nil
}

func Sync() error {
	if Logger == nil {
		return nil
	}
	err := Logger.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTTY) {
		err = nil
	}
	if fileLogger != nil {
		err = errors.Join(err, fileLogger.Close())
	}
	return err
}

func customLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	levelString := "[" + level.CapitalString() + "]"
	enc.AppendString(levelString)
}

func customCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	if caller.Defined {
		enc.AppendString("[" + caller.TrimmedPath() + "]")
	} else {
		enc.AppendString("[undefined]")
	}
}

type ZapLogger struct {
	logger *zap.SugaredLogger
}

func NewZapLogger(logger *zap.SugaredLogger) *ZapLogger {
	return &ZapLogger{logger: logger}
}

func (l *ZapLogger) GetLogger() *zap.SugaredLogger {
	return l.logger
}

// Printf formats according to a format specifier and writes to the logger.
func (l *ZapLogger) Printf(format string, v ...any) {
	l.logger.Infof(format, v...)
}

// Print calls Printf with the default message format.
func (l *ZapLogger) Print(v ...any) {
	l.logger.Info(v...)
}

// Println calls Print with a newline.
func (l *ZapLogger) Println(v ...any) {
	l.logger.Info(v...)
}

// Fatal calls Print followed by a call to os.Exit(1).
func (l *ZapLogger) Fatal(v ...any) {
	l.logger.Fatal(v...)
}

// Fatalf is equivalent to Printf followed by a call to os.Exit(1).
func (l *ZapLogger) Fatalf(format string, v ...any) {
	l.logger.Fatalf(format, v...)
}

// Fatalln is equivalent to Fatal.
func (l *ZapLogger) Fatalln(v ...any) {
	l.logger.Fatal(v...)
}

// Panic is equivalent to Print followed by a call to panic().
func (l *ZapLogger) Panic(v ...any) {
	l.logger.Panic(v...)
}

// Panicf is equivalent to Printf followed by a call to panic().
func (l *ZapLogger) Panicf(format string, v ...any) {
	l.logger.Panicf(format, v...)
}

func (l *ZapLogger) Debugf(format string, args ...any) {
	l.logger.Debugf(format, args...)
}

func (l *ZapLogger) Infof(format string, args ...any) {
	l.logger.Infof(format, args...)
}

func (l *ZapLogger) Warnf(format string, args ...any) {
	l.logger.Warnf(format, args...)
}

func (l *ZapLogger) Errorf(format string, args ...any) {
	l.logger.Errorf(format, args...)
}

func (l *ZapLogger) Debug(format string, args ...any) {
	l.logger.Debugf(format, args...)
}

func (l *ZapLogger) Info(format string, args ...any) {
	l.logger.Infof(format, args...)
}

func (l *ZapLogger) Warn(format string, args ...any) {
	l.logger.Warnf(format, args...)
}

func (l *ZapLogger) Error(format string, args ...any) {
	l.logger.Errorf(format, args...)
}

func (l *ZapLogger) Sync() error {
	return l.logger.Sync()
}

// FromContext 给业务和存储层日志附加请求标识，不记录原始请求头或请求体。
func FromContext(ctx context.Context) *zap.SugaredLogger {
	if Logger == nil {
		return zap.NewNop().Sugar()
	}
	return Logger.logger.With("request_id", requestinfo.ID(ctx))
}
