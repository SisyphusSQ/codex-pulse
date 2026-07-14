package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type runtimeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// HealthEvent 返回 fingerprint 聚合后的完整生命周期。
func (repository *Repository) HealthEvent(ctx context.Context, eventID string) (HealthEvent, error) {
	if repository == nil || repository.database == nil {
		return HealthEvent{}, ErrInvalidRepository
	}
	if eventID == "" {
		return HealthEvent{}, invalidRecord("health event ID must not be empty")
	}
	var event HealthEvent
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		event, found, err = healthEventByID(ctx, connection, eventID)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return event, err
}

// ListHealthEvents 使用受控 filter 返回健康生命周期事实。
func (repository *Repository) ListHealthEvents(
	ctx context.Context,
	filter HealthEventFilter,
) ([]HealthEvent, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateRuntimeLimit(filter.Limit)
	if err != nil {
		return nil, err
	}
	if filter.Severity != nil && !validHealthSeverity(*filter.Severity) {
		return nil, invalidRecord("health filter severity is invalid")
	}
	if err := validateOptionalStrings(filter.SourceFileID, filter.JobID); err != nil {
		return nil, err
	}
	query, arguments := buildHealthEventsQuery(filter, limit)

	var events []HealthEvent
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		rows, err := connection.QueryContext(ctx, query, arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			event, err := scanHealthEvent(rows)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, err
}

func healthEventByID(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, eventID string) (HealthEvent, bool, error) {
	return healthEventByColumn(ctx, querier, "event_id", eventID)
}

func healthEventByFingerprint(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, fingerprint string) (HealthEvent, bool, error) {
	return healthEventByColumn(ctx, querier, "fingerprint", fingerprint)
}

func healthEventByColumn(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, column, value string) (HealthEvent, bool, error) {
	query := `
		SELECT event_id, fingerprint, domain, severity, code, source_file_id, job_id,
			error_class, first_seen_at_ms, last_seen_at_ms, resolved_at_ms,
			occurrence_count, updated_at_ms
		FROM health_events WHERE ` + column + ` = ?`
	event, err := scanHealthEvent(querier.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return HealthEvent{}, false, nil
	}
	return event, err == nil, err
}

func scanHealthEvent(scanner runtimeRowScanner) (HealthEvent, error) {
	var event HealthEvent
	var fingerprint, domain, severity, code string
	var sourceFileID, jobID, errorClass sql.NullString
	var resolvedAt sql.NullInt64
	err := scanner.Scan(
		&event.EventID, &fingerprint, &domain, &severity, &code,
		&sourceFileID, &jobID, &errorClass, &event.FirstSeenAtMS, &event.LastSeenAtMS,
		&resolvedAt, &event.OccurrenceCount, &event.UpdatedAtMS,
	)
	if err != nil {
		return HealthEvent{}, err
	}
	parsedFingerprint, ok := parseSHA256Digest(fingerprint)
	if !ok {
		return HealthEvent{}, invalidRecord("stored health fingerprint is invalid")
	}
	event.Fingerprint = parsedFingerprint
	event.Domain = HealthDomain(domain)
	event.Severity = HealthSeverity(severity)
	event.Code = HealthCode(code)
	event.SourceFileID = stringPointer(sourceFileID)
	event.JobID = stringPointer(jobID)
	event.ErrorClass = runtimeErrorClassPointer(errorClass)
	event.ResolvedAtMS = int64Pointer(resolvedAt)
	return event, nil
}

// PricingVersion 返回不可变 catalog 及其完整规则集合。
func (repository *Repository) PricingVersion(ctx context.Context, versionID string) (PricingVersion, error) {
	if repository == nil || repository.database == nil {
		return PricingVersion{}, ErrInvalidRepository
	}
	if versionID == "" {
		return PricingVersion{}, invalidRecord("pricing version ID must not be empty")
	}
	var version PricingVersion
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var found bool
		var err error
		version, found, err = pricingVersionByID(ctx, connection, versionID)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return version, err
}

// PricingForModelAt 选择 source/currency/as-of 版本并稳定匹配模型规则。
func (repository *Repository) PricingForModelAt(
	ctx context.Context,
	source string,
	currency string,
	model string,
	atMS int64,
) (EffectivePricing, error) {
	if repository == nil || repository.database == nil {
		return EffectivePricing{}, ErrInvalidRepository
	}
	if source == "" || currency == "" || model == "" || atMS < 0 {
		return EffectivePricing{}, invalidRecord("pricing query identity or timestamp is invalid")
	}
	var effective EffectivePricing
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var versionID string
		var effectiveTo sql.NullInt64
		err := connection.QueryRowContext(
			ctx, effectivePricingVersionQuery, source, currency, atMS,
		).Scan(&versionID, &effectiveTo)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		version, found, err := pricingVersionByID(ctx, connection, versionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		candidates := make([]ModelPrice, 0, len(version.Models))
		for _, candidate := range version.Models {
			if modelPriceMatches(candidate, model) {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) == 0 {
			return ErrNotFound
		}
		sort.Slice(candidates, func(left, right int) bool {
			return modelPriceLess(candidates[left], candidates[right])
		})
		effective = EffectivePricing{
			PricingVersion: version,
			EffectiveToMS:  int64Pointer(effectiveTo),
			Matched:        candidates[0],
		}
		return nil
	})
	return effective, err
}

func pricingVersionByID(
	ctx context.Context,
	querier runtimeQuerier,
	versionID string,
) (PricingVersion, bool, error) {
	var version PricingVersion
	err := querier.QueryRowContext(ctx, `
		SELECT pricing_version, source, currency, effective_from_ms, created_at_ms
		FROM pricing_versions WHERE pricing_version = ?
	`, versionID).Scan(
		&version.PricingVersion, &version.Source, &version.Currency,
		&version.EffectiveFromMS, &version.CreatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PricingVersion{}, false, nil
	}
	if err != nil {
		return PricingVersion{}, false, err
	}
	rows, err := querier.QueryContext(ctx, pricingVersionModelsQuery, versionID)
	if err != nil {
		return PricingVersion{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		model, err := scanModelPrice(rows)
		if err != nil {
			return PricingVersion{}, false, err
		}
		version.Models = append(version.Models, model)
	}
	if err := rows.Err(); err != nil {
		return PricingVersion{}, false, err
	}
	return version, true, nil
}

func scanModelPrice(scanner runtimeRowScanner) (ModelPrice, error) {
	var model ModelPrice
	var matchKind string
	var input, cachedInput, output sql.NullInt64
	err := scanner.Scan(
		&matchKind, &model.ModelPattern, &model.Priority, &input, &cachedInput, &output,
	)
	if err != nil {
		return ModelPrice{}, err
	}
	model.MatchKind = ModelMatchKind(matchKind)
	model.InputMicrosPerMillion = int64Pointer(input)
	model.CachedInputMicrosPerMillion = int64Pointer(cachedInput)
	model.OutputMicrosPerMillion = int64Pointer(output)
	return model, nil
}

func pricingVersionsEquivalent(left, right PricingVersion) bool {
	if left.PricingVersion != right.PricingVersion || left.Source != right.Source ||
		left.Currency != right.Currency || left.EffectiveFromMS != right.EffectiveFromMS ||
		left.CreatedAtMS != right.CreatedAtMS || len(left.Models) != len(right.Models) {
		return false
	}
	leftModels := append([]ModelPrice(nil), left.Models...)
	rightModels := append([]ModelPrice(nil), right.Models...)
	sort.Slice(leftModels, func(i, j int) bool { return modelPriceIdentityLess(leftModels[i], leftModels[j]) })
	sort.Slice(rightModels, func(i, j int) bool { return modelPriceIdentityLess(rightModels[i], rightModels[j]) })
	for index := range leftModels {
		if !modelPricesEqual(leftModels[index], rightModels[index]) {
			return false
		}
	}
	return true
}

func modelPricesEqual(left, right ModelPrice) bool {
	return left.MatchKind == right.MatchKind && left.ModelPattern == right.ModelPattern &&
		left.Priority == right.Priority &&
		equalInt64Pointer(left.InputMicrosPerMillion, right.InputMicrosPerMillion) &&
		equalInt64Pointer(left.CachedInputMicrosPerMillion, right.CachedInputMicrosPerMillion) &&
		equalInt64Pointer(left.OutputMicrosPerMillion, right.OutputMicrosPerMillion)
}

func modelPriceIdentityLess(left, right ModelPrice) bool {
	if left.MatchKind != right.MatchKind {
		return left.MatchKind < right.MatchKind
	}
	return left.ModelPattern < right.ModelPattern
}

func modelPriceMatches(price ModelPrice, model string) bool {
	switch price.MatchKind {
	case ModelMatchExact:
		return model == price.ModelPattern
	case ModelMatchPrefix:
		return strings.HasPrefix(model, price.ModelPattern)
	case ModelMatchDefault:
		return true
	default:
		return false
	}
}

func modelPriceLess(left, right ModelPrice) bool {
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	leftRank := modelMatchRank(left.MatchKind)
	rightRank := modelMatchRank(right.MatchKind)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if len(left.ModelPattern) != len(right.ModelPattern) {
		return len(left.ModelPattern) > len(right.ModelPattern)
	}
	return left.ModelPattern < right.ModelPattern
}

func modelMatchRank(kind ModelMatchKind) int {
	switch kind {
	case ModelMatchExact:
		return 0
	case ModelMatchPrefix:
		return 1
	default:
		return 2
	}
}
