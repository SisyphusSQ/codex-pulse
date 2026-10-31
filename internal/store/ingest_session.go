package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ActiveSessionGeneration 返回 Session 唯一的已发布代次，供物理来源替换时建立 CAS lineage。
func (repository *Repository) ActiveSessionGeneration(ctx context.Context, sessionID string) (GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return GenerationCursor{}, ErrInvalidRepository
	}
	if sessionID == "" {
		return GenerationCursor{}, invalidRecord("session ID must not be empty")
	}
	var cursor GenerationCursor
	err := repository.database.View(ctx, func(ctx context.Context, db *gorm.DB) error {
		var generation sourceGenerationModel
		if err := db.WithContext(ctx).Where("session_id = ? AND state = ?", sessionID, GenerationActive).Take(&generation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var found bool
		var err error
		cursor, found, err = generationCursorByID(ctx, db, generation.SourceFileID, generation.Generation)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return cursor, err
}
