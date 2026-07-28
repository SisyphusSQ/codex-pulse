package store

import (
	"testing"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

type lightTestRepository struct {
	*coreTestRepository
	*lightIndexTestRepository
}

type coreTestRepository struct {
	*Repository
}

type lightIndexTestRepository struct {
	*storelight.Repository
}

func openLightRuntimeRepository(t testing.TB) *lightTestRepository {
	t.Helper()
	repository := openRuntimeRepository(t)
	return &lightTestRepository{
		coreTestRepository:       &coreTestRepository{Repository: repository},
		lightIndexTestRepository: &lightIndexTestRepository{Repository: storelight.NewRepository(repository.database)},
	}
}

type lightTokenTimedTestModel struct {
	InputTokens       int64 `gorm:"column:input_tokens"`
	CachedInputTokens int64 `gorm:"column:cached_input_tokens"`
	OutputTokens      int64 `gorm:"column:output_tokens"`
	ReasoningTokens   int64 `gorm:"column:reasoning_tokens"`
}

func (lightTokenTimedTestModel) TableName() string { return "light_token_timed" }
