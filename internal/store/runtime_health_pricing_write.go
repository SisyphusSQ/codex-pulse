package store

import (
	"context"
	"database/sql"
	"errors"

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
				ctx, transaction, `SELECT 1 FROM source_files WHERE source_file_id = ?`,
				*observation.SourceFileID, "source file",
			); err != nil {
				return err
			}
		}
		if observation.JobID != nil {
			if err := requireStoredReference(
				ctx, transaction, `SELECT 1 FROM job_runs WHERE job_id = ?`,
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
			_, err = transaction.ExecContext(ctx, `
				INSERT INTO health_events (
					event_id, fingerprint, domain, severity, code, source_file_id, job_id,
					error_class, first_seen_at_ms, last_seen_at_ms, resolved_at_ms,
					occurrence_count, updated_at_ms
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 1, ?)
			`,
				result.EventID, result.Fingerprint.String(), string(result.Domain), string(result.Severity), string(result.Code),
				nullableString(result.SourceFileID), nullableString(result.JobID),
				nullableRuntimeErrorClass(result.ErrorClass), result.FirstSeenAtMS,
				result.LastSeenAtMS, result.UpdatedAtMS,
			)
			return err
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
		_, err = transaction.ExecContext(ctx, `
			UPDATE health_events SET
				severity = ?, error_class = ?, last_seen_at_ms = ?, resolved_at_ms = NULL,
				occurrence_count = ?, updated_at_ms = ?
			WHERE event_id = ?
		`,
			string(result.Severity), nullableRuntimeErrorClass(result.ErrorClass), result.LastSeenAtMS,
			result.OccurrenceCount, result.UpdatedAtMS, result.EventID,
		)
		return err
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
		_, err = transaction.ExecContext(ctx, `
			UPDATE health_events SET resolved_at_ms = ?, updated_at_ms = ? WHERE event_id = ?
		`, resolvedAtMS, resolvedAtMS, eventID)
		return err
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
		var boundaryVersion string
		err = transaction.QueryRowContext(ctx, `
			SELECT pricing_version FROM pricing_versions
			WHERE source = ? AND currency = ? AND effective_from_ms = ?
		`, version.Source, version.Currency, version.EffectiveFromMS).Scan(&boundaryVersion)
		if err == nil {
			return invalidRecord("pricing effective boundary already belongs to another version")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO pricing_versions (
				pricing_version, source, currency, effective_from_ms, created_at_ms
			) VALUES (?, ?, ?, ?, ?)
		`,
			version.PricingVersion, version.Source, version.Currency,
			version.EffectiveFromMS, version.CreatedAtMS,
		); err != nil {
			return err
		}
		for _, model := range version.Models {
			if _, err := transaction.ExecContext(ctx, `
				INSERT INTO model_prices (
					pricing_version, match_kind, model_pattern, priority,
					input_micros_per_million, cached_input_micros_per_million,
					output_micros_per_million
				) VALUES (?, ?, ?, ?, ?, ?, ?)
			`,
				version.PricingVersion, string(model.MatchKind), model.ModelPattern, model.Priority,
				nullableInt64(model.InputMicrosPerMillion),
				nullableInt64(model.CachedInputMicrosPerMillion),
				nullableInt64(model.OutputMicrosPerMillion),
			); err != nil {
				return err
			}
		}
		return nil
	})
}
