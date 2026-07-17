package store

import (
	"context"
	"strings"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const maxHealthEvaluationManagedEvents = 64

const HealthEvaluatorEventIDPrefix = "health-evaluator-"

// HealthEvaluationSnapshot 在同一个 SQLite read transaction 中读取 metrics 与 lifecycle。
func (repository *Repository) HealthEvaluationSnapshot(
	ctx context.Context,
	filter MetricsSnapshotFilter,
	managedEvents []HealthManagedEvent,
) (HealthEvaluationSnapshot, error) {
	if repository == nil || repository.database == nil {
		return HealthEvaluationSnapshot{}, ErrInvalidRepository
	}
	if err := validateMetricsSnapshotFilter(filter); err != nil {
		return HealthEvaluationSnapshot{}, err
	}
	managed, err := validateHealthManagedEvents(managedEvents)
	if err != nil {
		return HealthEvaluationSnapshot{}, err
	}
	result := HealthEvaluationSnapshot{Metrics: MetricsSnapshot{FromMS: filter.FromMS, UntilMS: filter.UntilMS}}
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			if err := repository.readMetricsSnapshotIn(ctx, transaction, filter, &result.Metrics); err != nil {
				return err
			}
			lifecycle, found, err := schedulerLifecycleIn(ctx, transaction)
			if err != nil {
				return err
			}
			if found {
				result.Lifecycle = &lifecycle
			}
			return readActiveHealthMetrics(ctx, transaction, managedEvents, managed, &result)
		})
	})
	return result, err
}

type activeHealthMetricRow struct {
	Domain   string `gorm:"column:domain"`
	Severity string `gorm:"column:severity"`
	Code     string `gorm:"column:code"`
	Count    int64  `gorm:"column:count"`
}

func readActiveHealthMetrics(
	ctx context.Context,
	transaction *gorm.DB,
	managedEvents []HealthManagedEvent,
	managed map[string]HealthManagedEvent,
	snapshot *HealthEvaluationSnapshot,
) error {
	managedIDs := make([]string, len(managedEvents))
	for index, descriptor := range managedEvents {
		managedIDs[index] = descriptor.EventID
	}
	var candidates []healthEventModel
	if err := transaction.WithContext(ctx).Model(&healthEventModel{}).
		Where("resolved_at_ms IS NULL AND event_id IN ?", managedIDs).Find(&candidates).Error; err != nil {
		return err
	}
	for _, model := range candidates {
		event, err := healthEventFromModel(model)
		if err != nil {
			return err
		}
		descriptor, ok := managed[event.EventID]
		if !ok || !healthEventMatchesManaged(event, descriptor) {
			return invalidRecord("health evaluator event ownership conflicts")
		}
	}
	var rows []activeHealthMetricRow
	if err := transaction.WithContext(ctx).Model(&healthEventModel{}).
		Select("domain, severity, code, COUNT(*) AS count").
		Where("resolved_at_ms IS NULL AND event_id NOT IN ?", managedIDs).
		Group("domain, severity, code").Order("domain, severity, code").Scan(&rows).Error; err != nil {
		return err
	}
	snapshot.ActiveHealth = make([]HealthEventMetric, len(rows))
	for index, row := range rows {
		value := HealthEventMetric{
			Domain: HealthDomain(row.Domain), Severity: HealthSeverity(row.Severity),
			Code: HealthCode(row.Code), Count: row.Count,
		}
		if value.Count < 1 || !validHealthDomain(value.Domain) || !validHealthSeverity(value.Severity) ||
			!validHealthCode(value.Domain, value.Code) {
			return invalidRecord("active health metric is invalid")
		}
		snapshot.ActiveHealth[index] = value
	}
	return nil
}

// ApplyHealthEvaluationBatch 原子观察活跃事件并解析本轮不再活跃的受管事件。
func (repository *Repository) ApplyHealthEvaluationBatch(ctx context.Context, batch HealthEvaluationBatch) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	managed, active, err := validateHealthEvaluationBatch(batch)
	if err != nil {
		return err
	}
	return repository.database.WriteMaintenance(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		for _, observation := range batch.Observations {
			if _, err := observeHealthEventIn(ctx, transaction, observation); err != nil {
				return err
			}
		}
		if repository.healthEvaluationWriteHook != nil {
			if err := repository.healthEvaluationWriteHook("observed"); err != nil {
				return err
			}
		}
		for _, descriptor := range batch.ManagedEvents {
			if _, current := active[descriptor.EventID]; current {
				continue
			}
			if err := resolveManagedHealthEventIn(
				ctx, transaction, managed[descriptor.EventID], batch.EvaluatedAtMS,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func validateHealthEvaluationBatch(
	batch HealthEvaluationBatch,
) (map[string]HealthManagedEvent, map[string]struct{}, error) {
	if batch.EvaluatedAtMS < 0 {
		return nil, nil, invalidRecord("health evaluation batch boundary is invalid")
	}
	managed, err := validateHealthManagedEvents(batch.ManagedEvents)
	if err != nil {
		return nil, nil, err
	}
	active := make(map[string]struct{}, len(batch.Observations))
	for _, observation := range batch.Observations {
		if err := validateHealthObservation(observation); err != nil {
			return nil, nil, err
		}
		if observation.ObservedAtMS != batch.EvaluatedAtMS {
			return nil, nil, invalidRecord("health observation time differs from evaluation time")
		}
		descriptor, allowed := managed[observation.EventID]
		if !allowed || !healthObservationMatchesManaged(observation, descriptor) {
			return nil, nil, invalidRecord("health observation is outside managed ownership")
		}
		if _, duplicate := active[observation.EventID]; duplicate {
			return nil, nil, invalidRecord("health observation identity is duplicated")
		}
		active[observation.EventID] = struct{}{}
	}
	return managed, active, nil
}

func validateHealthManagedEvents(values []HealthManagedEvent) (map[string]HealthManagedEvent, error) {
	if len(values) == 0 || len(values) > maxHealthEvaluationManagedEvents {
		return nil, invalidRecord("health evaluation managed boundary is invalid")
	}
	managed := make(map[string]HealthManagedEvent, len(values))
	fingerprints := make(map[string]struct{}, len(values))
	for _, value := range values {
		fingerprint := value.Fingerprint.String()
		if !strings.HasPrefix(value.EventID, HealthEvaluatorEventIDPrefix) ||
			fingerprint == "" || fingerprint != SHA256DigestOf([]byte(value.EventID)).String() ||
			!validHealthDomain(value.Domain) || !validHealthCode(value.Domain, value.Code) {
			return nil, invalidRecord("health evaluation managed descriptor is invalid")
		}
		if _, duplicate := managed[value.EventID]; duplicate {
			return nil, invalidRecord("health evaluation managed identity is duplicated")
		}
		if _, duplicate := fingerprints[fingerprint]; duplicate {
			return nil, invalidRecord("health evaluation managed fingerprint is duplicated")
		}
		managed[value.EventID] = value
		fingerprints[fingerprint] = struct{}{}
	}
	return managed, nil
}

func healthObservationMatchesManaged(observation HealthObservation, descriptor HealthManagedEvent) bool {
	return observation.EventID == descriptor.EventID &&
		observation.Fingerprint.String() == descriptor.Fingerprint.String() &&
		observation.Domain == descriptor.Domain && observation.Code == descriptor.Code &&
		observation.SourceFileID == nil && observation.JobID == nil && observation.ErrorClass == nil
}

func healthEventMatchesManaged(event HealthEvent, descriptor HealthManagedEvent) bool {
	return event.EventID == descriptor.EventID &&
		event.Fingerprint.String() == descriptor.Fingerprint.String() &&
		event.Domain == descriptor.Domain && event.Code == descriptor.Code &&
		event.SourceFileID == nil && event.JobID == nil && event.ErrorClass == nil
}

func resolveManagedHealthEventIn(
	ctx context.Context,
	transaction *gorm.DB,
	descriptor HealthManagedEvent,
	resolvedAtMS int64,
) error {
	event, found, err := healthEventByID(ctx, transaction, descriptor.EventID)
	if err != nil || !found {
		return err
	}
	if !healthEventMatchesManaged(event, descriptor) {
		return invalidRecord("health evaluator event ownership conflicts")
	}
	return resolveHealthEventIn(ctx, transaction, descriptor.EventID, resolvedAtMS, true)
}
