package gormv2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm/logger"

	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
)

// queryLogger 关联请求，只记录时长/行数/错误类型，不记录 SQL 参数或凭据。
type queryLogger struct{ level logger.LogLevel }

func (l queryLogger) LogMode(level logger.LogLevel) logger.Interface { l.level = level; return l }
func (l queryLogger) Info(ctx context.Context, _ string, _ ...any) {
	if l.level >= logger.Info {
		log.FromContext(ctx).Info("database operation")
	}
}
func (l queryLogger) Warn(ctx context.Context, _ string, _ ...any) {
	if l.level >= logger.Warn {
		log.FromContext(ctx).Warn("database warning")
	}
}
func (l queryLogger) Error(ctx context.Context, _ string, _ ...any) {
	if l.level >= logger.Error {
		log.FromContext(ctx).Error("database error")
	}
}
func (l queryLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level == logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	if err == nil && elapsed <= 200*time.Millisecond && l.level < logger.Info {
		return
	}
	_, rows := fc()
	entry := log.FromContext(ctx)
	if err != nil && !errors.Is(err, ErrRecordNotFound) && l.level >= logger.Error {
		entry.Errorw("database query failed", "duration", elapsed, "rows", rows, "error_type", fmt.Sprintf("%T", err))
		return
	}
	if elapsed > 200*time.Millisecond && l.level >= logger.Warn {
		entry.Warnw("slow database query", "duration", elapsed, "rows", rows)
		return
	}
	if l.level >= logger.Info {
		entry.Infow("database query completed", "duration", elapsed, "rows", rows)
	}
}
