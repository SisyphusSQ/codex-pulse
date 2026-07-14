package store

import (
	"context"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// ObserveHealthEvent 按 fingerprint 合并递增观测，并在新观测到达时重开已解决事件。
func (repository *Repository) ObserveHealthEvent(
	ctx context.Context,
	observation HealthObservation,
) (HealthEvent, error) {
	if repository == nil || repository.database == nil {
		return HealthEvent{}, ErrInvalidRepository
	}
	if err := validateHealthObservation(observation); err != nil {
		return HealthEvent{}, err
	}
	var result HealthEvent
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if observation.SourceFileID != nil {
			if err := requireStoredReference(
				ctx, transaction, &sourceFileModel{}, "source_file_id = ?",
				*observation.SourceFileID, "source file",
			); err != nil {
				return err
			}
		}
		if observation.JobID != nil {
			if err := requireStoredReference(
				ctx, transaction, &jobRunModel{}, "job_id = ?",
				*observation.JobID, "job run",
			); err != nil {
				return err
			}
		}

		existing, found, err := healthEventByFingerprint(ctx, transaction, observation.Fingerprint.String())
		if err != nil {
			return err
		}
		if !found {
			_, idFound, err := healthEventByID(ctx, transaction, observation.EventID)
			if err != nil {
				return err
			}
			if idFound {
				return invalidRecord("health event ID belongs to another fingerprint")
			}
			result = HealthEvent{
				EventID: observation.EventID, Fingerprint: observation.Fingerprint,
				Domain: observation.Domain, Severity: observation.Severity, Code: observation.Code,
				SourceFileID: observation.SourceFileID, JobID: observation.JobID,
				ErrorClass: observation.ErrorClass, FirstSeenAtMS: observation.ObservedAtMS,
				LastSeenAtMS: observation.ObservedAtMS, OccurrenceCount: 1, UpdatedAtMS: observation.ObservedAtMS,
			}
			model := healthEventModelFromDomain(result)
			return transaction.WithContext(ctx).Create(&model).Error
		}

		if existing.EventID != observation.EventID || existing.Domain != observation.Domain ||
			existing.Code != observation.Code ||
			!equalStringPointer(existing.SourceFileID, observation.SourceFileID) ||
			!equalStringPointer(existing.JobID, observation.JobID) {
			return invalidRecord("health fingerprint stable identity conflicts")
		}
		if observation.ObservedAtMS == existing.LastSeenAtMS {
			if existing.Severity == observation.Severity &&
				equalRuntimeErrorClassPointer(existing.ErrorClass, observation.ErrorClass) {
				result = existing
				return nil
			}
			return invalidRecord("health observation conflicts at the same time")
		}
		if existing.ResolvedAtMS != nil && observation.ObservedAtMS <= *existing.ResolvedAtMS {
			return invalidRecord("health observation does not advance resolved lifecycle")
		}
		if observation.ObservedAtMS < existing.UpdatedAtMS {
			return invalidRecord("health observation time regresses lifecycle ordering")
		}
		if observation.ObservedAtMS < existing.LastSeenAtMS {
			return invalidRecord("health observation time regresses")
		}

		result = existing
		result.Severity = observation.Severity
		result.ErrorClass = observation.ErrorClass
		result.LastSeenAtMS = observation.ObservedAtMS
		result.ResolvedAtMS = nil
		result.OccurrenceCount++
		result.UpdatedAtMS = observation.ObservedAtMS
		return transaction.WithContext(ctx).Model(&healthEventModel{}).
			Where("event_id = ?", result.EventID).Updates(map[string]any{
			"severity": string(result.Severity), "error_class": runtimeErrorStringPointer(result.ErrorClass),
			"last_seen_at_ms": result.LastSeenAtMS, "resolved_at_ms": nil,
			"occurrence_count": result.OccurrenceCount, "updated_at_ms": result.UpdatedAtMS,
		}).Error
	})
	return result, err
}

// ResolveHealthEvent 以不早于 last_seen 的时间终结当前健康事件生命周期。
func (repository *Repository) ResolveHealthEvent(ctx context.Context, eventID string, resolvedAtMS int64) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if eventID == "" || resolvedAtMS < 0 {
		return invalidRecord("health resolution identity or timestamp is invalid")
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		event, found, err := healthEventByID(ctx, transaction, eventID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if resolvedAtMS < event.LastSeenAtMS {
			return invalidRecord("health resolution precedes the last observation")
		}
		if event.ResolvedAtMS != nil {
			if *event.ResolvedAtMS == resolvedAtMS {
				return nil
			}
			return invalidRecord("health resolution conflicts with existing lifecycle")
		}
		return transaction.WithContext(ctx).Model(&healthEventModel{}).
			Where("event_id = ?", eventID).
			Updates(map[string]any{"resolved_at_ms": resolvedAtMS, "updated_at_ms": resolvedAtMS}).Error
	})
}

// AddPricingVersion 原子追加不可变版本和完整模型规则集合。
func (repository *Repository) AddPricingVersion(ctx context.Context, version PricingVersion) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validatePricingVersion(version); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := pricingVersionByID(ctx, transaction, version.PricingVersion)
		if err != nil {
			return err
		}
		if found {
			if pricingVersionsEquivalent(existing, version) {
				return nil
			}
			return invalidRecord("pricing version conflicts with immutable history")
		}
		var boundaryCount int64
		err = transaction.WithContext(ctx).Model(&pricingVersionModel{}).
			Where("source = ? AND currency = ? AND effective_from_ms = ?", version.Source, version.Currency, version.EffectiveFromMS).
			Count(&boundaryCount).Error
		if err != nil {
			return err
		}
		if boundaryCount > 0 {
			return invalidRecord("pricing effective boundary already belongs to another version")
		}
		versionModel := pricingVersionModel{
			PricingVersion: version.PricingVersion, Source: version.Source, Currency: version.Currency,
			EffectiveFromMS: version.EffectiveFromMS, CreatedAtMS: version.CreatedAtMS,
		}
		if err := transaction.WithContext(ctx).Create(&versionModel).Error; err != nil {
			return err
		}
		models := make([]modelPriceModel, 0, len(version.Models))
		for _, model := range version.Models {
			models = append(models, modelPriceModel{
				PricingVersion: version.PricingVersion, MatchKind: string(model.MatchKind),
				ModelPattern: model.ModelPattern, Priority: model.Priority,
				InputMicrosPerMillion:       model.InputMicrosPerMillion,
				CachedInputMicrosPerMillion: model.CachedInputMicrosPerMillion,
				OutputMicrosPerMillion:      model.OutputMicrosPerMillion,
			})
		}
		if err := transaction.WithContext(ctx).Create(&models).Error; err != nil {
			return err
		}
		if version.SourceURL == "" {
			return nil
		}
		return transaction.WithContext(ctx).Create(&pricingCatalogMetadataModel{
			PricingVersion: version.PricingVersion,
			SourceURL:      version.SourceURL,
			VerifiedAtMS:   version.VerifiedAtMS,
		}).Error
	})
}

func healthEventModelFromDomain(event HealthEvent) healthEventModel {
	return healthEventModel{
		EventID: event.EventID, Fingerprint: event.Fingerprint.String(), Domain: string(event.Domain),
		Severity: string(event.Severity), Code: string(event.Code), SourceFileID: event.SourceFileID,
		JobID: event.JobID, ErrorClass: runtimeErrorStringPointer(event.ErrorClass),
		FirstSeenAtMS: event.FirstSeenAtMS, LastSeenAtMS: event.LastSeenAtMS,
		ResolvedAtMS: event.ResolvedAtMS, OccurrenceCount: event.OccurrenceCount,
		UpdatedAtMS: event.UpdatedAtMS,
	}
}
