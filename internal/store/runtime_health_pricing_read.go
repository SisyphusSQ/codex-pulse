package store

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

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

	var events []HealthEvent
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&healthEventModel{})
		if filter.Active != nil {
			if *filter.Active {
				query = query.Where("resolved_at_ms IS NULL")
			} else {
				query = query.Where("resolved_at_ms IS NOT NULL")
			}
		}
		if filter.Severity != nil {
			query = query.Where("severity = ?", string(*filter.Severity))
		}
		if filter.SourceFileID != nil {
			query = query.Where("source_file_id = ?", *filter.SourceFileID)
		}
		if filter.JobID != nil {
			query = query.Where("job_id = ?", *filter.JobID)
		}
		var models []healthEventModel
		if err := query.Order("last_seen_at_ms DESC").Order("event_id").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		events = make([]HealthEvent, 0, len(models))
		for _, model := range models {
			event, err := healthEventFromModel(model)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return nil
	})
	return events, err
}

func healthEventByID(ctx context.Context, querier *gorm.DB, eventID string) (HealthEvent, bool, error) {
	return healthEventByWhere(ctx, querier, "event_id = ?", eventID)
}

func healthEventByFingerprint(ctx context.Context, querier *gorm.DB, fingerprint string) (HealthEvent, bool, error) {
	return healthEventByWhere(ctx, querier, "fingerprint = ?", fingerprint)
}

func healthEventByWhere(
	ctx context.Context,
	querier *gorm.DB,
	condition string,
	value string,
) (HealthEvent, bool, error) {
	var model healthEventModel
	err := querier.WithContext(ctx).Where(condition, value).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return HealthEvent{}, false, nil
	}
	if err != nil {
		return HealthEvent{}, false, err
	}
	event, err := healthEventFromModel(model)
	return event, err == nil, err
}

func healthEventFromModel(model healthEventModel) (HealthEvent, error) {
	fingerprint, ok := parseSHA256Digest(model.Fingerprint)
	if !ok {
		return HealthEvent{}, invalidRecord("stored health fingerprint is invalid")
	}
	return HealthEvent{
		EventID: model.EventID, Fingerprint: fingerprint, Domain: HealthDomain(model.Domain),
		Severity: HealthSeverity(model.Severity), Code: HealthCode(model.Code),
		SourceFileID: model.SourceFileID, JobID: model.JobID,
		ErrorClass: runtimeErrorClassFromString(model.ErrorClass), FirstSeenAtMS: model.FirstSeenAtMS,
		LastSeenAtMS: model.LastSeenAtMS, ResolvedAtMS: model.ResolvedAtMS,
		OccurrenceCount: model.OccurrenceCount, UpdatedAtMS: model.UpdatedAtMS,
	}, nil
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
		var current pricingVersionModel
		err := connection.WithContext(ctx).
			Where("source = ? AND currency = ? AND effective_from_ms <= ?", source, currency, atMS).
			Order("effective_from_ms DESC").Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		version, found, err := pricingVersionByID(ctx, connection, current.PricingVersion)
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

		var next pricingVersionModel
		nextQuery := connection.WithContext(ctx).
			Select("effective_from_ms").
			Where("source = ? AND currency = ? AND effective_from_ms > ?", source, currency, current.EffectiveFromMS).
			Order("effective_from_ms").Limit(1).Find(&next)
		if nextQuery.Error != nil {
			return nextQuery.Error
		}
		var effectiveToMS *int64
		if nextQuery.RowsAffected > 0 {
			effectiveToMS = &next.EffectiveFromMS
		}
		effective = EffectivePricing{
			PricingVersion: version,
			EffectiveToMS:  effectiveToMS,
			Matched:        candidates[0],
		}
		return nil
	})
	return effective, err
}

func pricingVersionByID(
	ctx context.Context,
	querier *gorm.DB,
	versionID string,
) (PricingVersion, bool, error) {
	var versionModel pricingVersionModel
	err := querier.WithContext(ctx).Where("pricing_version = ?", versionID).Take(&versionModel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PricingVersion{}, false, nil
	}
	if err != nil {
		return PricingVersion{}, false, err
	}
	var priceModels []modelPriceModel
	if err := querier.WithContext(ctx).
		Where("pricing_version = ?", versionID).
		Order("priority DESC").Order("match_kind").Order("model_pattern").
		Find(&priceModels).Error; err != nil {
		return PricingVersion{}, false, err
	}
	version := PricingVersion{
		PricingVersion: versionModel.PricingVersion, Source: versionModel.Source,
		Currency: versionModel.Currency, EffectiveFromMS: versionModel.EffectiveFromMS,
		CreatedAtMS: versionModel.CreatedAtMS, Models: make([]ModelPrice, 0, len(priceModels)),
	}
	if querier.Migrator().HasTable(&pricingCatalogMetadataModel{}) {
		var metadata pricingCatalogMetadataModel
		metadataQuery := querier.WithContext(ctx).
			Where("pricing_version = ?", versionID).Limit(1).Find(&metadata)
		if metadataQuery.Error != nil {
			return PricingVersion{}, false, metadataQuery.Error
		}
		if metadataQuery.RowsAffected > 0 {
			version.SourceURL = metadata.SourceURL
			version.VerifiedAtMS = metadata.VerifiedAtMS
		}
	}
	for _, model := range priceModels {
		version.Models = append(version.Models, modelPriceFromModel(model))
	}
	return version, true, nil
}

func modelPriceFromModel(model modelPriceModel) ModelPrice {
	return ModelPrice{
		MatchKind: ModelMatchKind(model.MatchKind), ModelPattern: model.ModelPattern,
		Priority: model.Priority, InputMicrosPerMillion: model.InputMicrosPerMillion,
		CachedInputMicrosPerMillion: model.CachedInputMicrosPerMillion,
		OutputMicrosPerMillion:      model.OutputMicrosPerMillion,
	}
}

func pricingVersionsEquivalent(left, right PricingVersion) bool {
	if left.PricingVersion != right.PricingVersion || left.Source != right.Source ||
		left.Currency != right.Currency || left.EffectiveFromMS != right.EffectiveFromMS ||
		left.CreatedAtMS != right.CreatedAtMS || left.SourceURL != right.SourceURL ||
		left.VerifiedAtMS != right.VerifiedAtMS || len(left.Models) != len(right.Models) {
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
