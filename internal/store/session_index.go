package store

import (
	"context"
	"errors"
	"slices"
	"strings"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

var ErrSessionIndexExpectationDrift = errors.New("session index expectation snapshot drift")

// SessionIndexExpectation 是 repair analyzer 可使用的带字段时间 Session name projection。
type SessionIndexExpectation struct {
	SessionID   string
	ThreadName  string
	UpdatedAtMS int64
}

// ListSessionIndexExpectations 只读返回可证明完整的 Session name，并按 ID 稳定排序。
func (repository *Repository) ListSessionIndexExpectations(
	ctx context.Context,
) ([]SessionIndexExpectation, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	var expectations []SessionIndexExpectation
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var queryErr error
		expectations, queryErr = listSessionIndexExpectations(ctx, connection)
		return queryErr
	})
	return expectations, err
}

// CompleteSessionIndexRepairJob 在同一 writer transaction 内验证 Session name
// projection 与确认快照完全一致，再把 repair job 迁移为 succeeded。
func (repository *Repository) CompleteSessionIndexRepairJob(
	ctx context.Context,
	expected []SessionIndexExpectation,
	transition JobTransition,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateSessionIndexExpectationSnapshot(expected); err != nil {
		return err
	}
	if err := validateJobTransition(transition); err != nil {
		return err
	}
	if transition.ExpectedState != JobRunning || transition.State != JobSucceeded {
		return invalidRecord("session index repair completion must transition running to succeeded")
	}
	expected = slices.Clone(expected)
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		current, err := listSessionIndexExpectations(ctx, transaction)
		if err != nil {
			return err
		}
		if !slices.Equal(current, expected) {
			return ErrSessionIndexExpectationDrift
		}
		return transitionJobRun(ctx, transaction, transition)
	})
}

func listSessionIndexExpectations(
	ctx context.Context,
	connection *gorm.DB,
) ([]SessionIndexExpectation, error) {
	var expectations []SessionIndexExpectation
	err := connection.WithContext(ctx).
		Model(&sessionCurrentModel{}).
		Select("session_current.session_id, session_current.thread_name, session_current.thread_name_updated_at_ms AS updated_at_ms").
		Joins("JOIN sessions ON sessions.session_id = session_current.session_id").
		Where("session_current.thread_name IS NOT NULL AND session_current.thread_name_updated_at_ms IS NOT NULL").
		Order("session_current.session_id").
		Scan(&expectations).Error
	return expectations, err
}

func validateSessionIndexExpectationSnapshot(expectations []SessionIndexExpectation) error {
	previousSessionID := ""
	for _, expectation := range expectations {
		if expectation.SessionID == "" || expectation.SessionID <= previousSessionID ||
			strings.TrimSpace(expectation.ThreadName) == "" || expectation.UpdatedAtMS < 0 {
			return invalidRecord("invalid session index expectation snapshot")
		}
		previousSessionID = expectation.SessionID
	}
	return nil
}
