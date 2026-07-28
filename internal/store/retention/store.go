package retention

import (
	"errors"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// ErrInvalidRepository 表示 retention 仓储未绑定应用 SQLite Store。
var ErrInvalidRepository = errors.New("invalid retention repository")

// Repository 执行有界、低优先级的运行事实清理。
type Repository struct {
	database *storesqlite.Store
}

// NewRepository 使用已有 Store 构造 retention 仓储，不取得连接生命周期所有权。
func NewRepository(database *storesqlite.Store) *Repository {
	return &Repository{database: database}
}
