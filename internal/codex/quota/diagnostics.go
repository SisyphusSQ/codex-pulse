package quota

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// DiagnosticReason 对本地写入/运行时错误归类；绝不记录原始 Error 文本。
func DiagnosticReason(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, store.ErrCodexAccountBindingChanged):
		return "account_identity_unavailable"
	case errors.Is(err, store.ErrInvalidRecord):
		return "invalid_record"
	case errors.Is(err, storesqlite.ErrBusy):
		return "store_busy"
	case errors.Is(err, storesqlite.ErrIO):
		return "store_io"
	case errors.Is(err, storesqlite.ErrQueueFull):
		return "store_queue_full"
	case errors.Is(err, storesqlite.ErrDiskFull):
		return "store_full"
	case errors.Is(err, storesqlite.ErrReadOnly):
		return "store_read_only"
	case errors.Is(err, storesqlite.ErrPermission):
		return "store_permission"
	case errors.Is(err, storesqlite.ErrCorrupt):
		return "store_corrupt"
	case errors.Is(err, storesqlite.ErrClosed), errors.Is(err, storesqlite.ErrClosing):
		return "store_closed"
	default:
		return "internal_error"
	}
}
