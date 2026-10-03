package log

import (
	"context"
	"time"

	"gorm.io/gorm/logger"
)

type SilentLogger struct{}

// LogMode 实现 Logger 接口
func (s SilentLogger) LogMode(logger.LogLevel) logger.Interface {
	return s
}

// Info 实现 Logger 接口
func (s SilentLogger) Info(ctx context.Context, msg string, data ...interface{}) {}

// Warn 实现 Logger 接口
func (s SilentLogger) Warn(ctx context.Context, msg string, data ...interface{}) {}

// Error 实现 Logger 接口
func (s SilentLogger) Error(ctx context.Context, msg string, data ...interface{}) {}

// Trace 实现 Logger 接口
func (s SilentLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
}
