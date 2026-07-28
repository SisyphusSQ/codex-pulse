package lightindex

import (
	"errors"
	"fmt"
	"math"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	// ErrInvalidRepository 表示仓储未绑定应用 SQLite Store。
	ErrInvalidRepository = errors.New("invalid light index repository")
	// ErrInvalidRecord 表示 light index 记录不满足持久化 contract。
	ErrInvalidRecord = errors.New("invalid light index record")
	// ErrNotFound 表示 light index 中不存在请求的记录。
	ErrNotFound = errors.New("light index record not found")
)

// Repository 持久化和查询轻量 Session metadata 与 token 索引。
type Repository struct {
	database *storesqlite.Store
}

// NewRepository 使用已有 Store 构造轻量索引仓储，不取得连接生命周期所有权。
func NewRepository(database *storesqlite.Store) *Repository {
	return &Repository{database: database}
}

func invalidRecord(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecord, message)
}

func checkedAdd(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, invalidRecord("integer total overflow")
	}
	return left + right, nil
}
