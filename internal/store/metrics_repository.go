package store

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// RecordAppRuntimeSample 通过 maintenance lane 写入一个 replay-safe runtime sample。
func (repository *Repository) RecordAppRuntimeSample(ctx context.Context, sample AppRuntimeSample) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateAppRuntimeSample(sample); err != nil {
		return err
	}
	return repository.database.WriteMaintenance(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		var existing appRuntimeSampleModel
		result := transaction.WithContext(ctx).Take(&existing, "captured_at_ms = ?", sample.CapturedAtMS)
		if result.Error == nil {
			if appRuntimeSampleFromModel(existing) == sample {
				return nil
			}
			return invalidRecord("app runtime sample conflicts at captured time")
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		model := appRuntimeSampleModelFromDomain(sample)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

// ListAppRuntimeSamples 返回半开窗口内按采样时间倒序排列的 runtime samples。
func (repository *Repository) ListAppRuntimeSamples(
	ctx context.Context,
	filter AppRuntimeSampleFilter,
) ([]AppRuntimeSample, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if err := validateAppRuntimeSampleFilter(filter); err != nil {
		return nil, err
	}
	var values []AppRuntimeSample
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []appRuntimeSampleModel
		if err := connection.WithContext(ctx).
			Where("captured_at_ms >= ? AND captured_at_ms < ?", filter.FromMS, filter.UntilMS).
			Order("captured_at_ms DESC").Limit(filter.Limit).Find(&models).Error; err != nil {
			return err
		}
		values = make([]AppRuntimeSample, len(models))
		for index, model := range models {
			value := appRuntimeSampleFromModel(model)
			if err := validateAppRuntimeSample(value); err != nil {
				return err
			}
			values[index] = value
		}
		return nil
	})
	return values, err
}

func validateAppRuntimeSample(value AppRuntimeSample) error {
	if value.CapturedAtMS < 0 || value.CapturedAtMS > runtimeclock.MaxTimestampMS ||
		math.IsNaN(value.CPUPercent) || math.IsInf(value.CPUPercent, 0) ||
		value.CPUPercent < 0 || value.CPUPercent > 102_400 ||
		value.CPUUserMS < 0 || value.CPUSystemMS < 0 ||
		value.RSSBytes < 0 || value.PeakRSSBytes < value.RSSBytes || value.GoroutineCount < 1 ||
		value.DBBytes < 0 || value.WALBytes < 0 || value.DiskFreeBytes < 0 ||
		value.LiveQueueDepth < 0 || value.BackfillQueueDepth < 0 ||
		value.OldestLiveWaitMS < 0 || value.OldestBackfillWaitMS < 0 ||
		value.QueryCount < 0 || value.QueryTotalMicros < 0 || value.QueryMaxMicros < 0 ||
		value.CollectorDurationMicros < 0 || value.DroppedSamples < 0 {
		return invalidRecord("app runtime sample contains invalid metrics")
	}
	if value.QueryCount == 0 {
		if value.QueryTotalMicros != 0 || value.QueryMaxMicros != 0 {
			return invalidRecord("empty query aggregate contains latency")
		}
	} else if value.QueryTotalMicros < value.QueryMaxMicros {
		return invalidRecord("query maximum exceeds total latency")
	}
	return nil
}

func validateAppRuntimeSampleFilter(filter AppRuntimeSampleFilter) error {
	if filter.FromMS < 0 || filter.UntilMS <= filter.FromMS ||
		filter.UntilMS > runtimeclock.MaxTimestampMS+1 ||
		filter.Limit < 1 || filter.Limit > MaxAppRuntimeSampleQueryLimit {
		return invalidRecord("app runtime sample filter is invalid")
	}
	return nil
}
