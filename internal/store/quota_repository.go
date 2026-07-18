package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func (repository *Repository) QuotaObservation(ctx context.Context, observationID string) (QuotaObservation, error) {
	if repository == nil || repository.database == nil {
		return QuotaObservation{}, ErrInvalidRepository
	}
	if observationID == "" {
		return QuotaObservation{}, invalidRecord("quota observation ID must not be empty")
	}
	var observation QuotaObservation
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var model quotaObservationModel
		err := connection.WithContext(ctx).Take(&model, "observation_id = ?", observationID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var mappingErr error
		observation, mappingErr = quotaObservationFromModel(model)
		return mappingErr
	})
	return observation, err
}

func (repository *Repository) ListQuotaObservations(
	ctx context.Context,
	filter QuotaObservationFilter,
) ([]QuotaObservation, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	limit, err := validateQuotaObservationFilter(filter)
	if err != nil {
		return nil, err
	}
	var observations []QuotaObservation
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		query := connection.WithContext(ctx).Model(&quotaObservationModel{})
		if filter.AccountScope != nil {
			query = query.Where("account_scope = ?", *filter.AccountScope)
		}
		if filter.Source != nil {
			query = query.Where("source = ?", string(*filter.Source))
		}
		if filter.WindowKind != nil {
			query = query.Where("window_kind = ?", string(*filter.WindowKind))
		}
		if filter.Validity != nil {
			query = query.Where("validity = ?", string(*filter.Validity))
		}
		if filter.SessionID != nil {
			query = query.Where("session_id = ?", *filter.SessionID)
		}
		if filter.SourceFileID != nil {
			query = query.Where("source_file_id = ?", *filter.SourceFileID)
		}
		var models []quotaObservationModel
		if err := query.Order("first_observed_at_ms").Order("observation_id").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		observations = make([]QuotaObservation, 0, len(models))
		for _, model := range models {
			observation, err := quotaObservationFromModel(model)
			if err != nil {
				return err
			}
			observations = append(observations, observation)
		}
		return nil
	})
	return observations, err
}

func validateQuotaObservationFilter(filter QuotaObservationFilter) (int, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 0 || limit > 500 {
		return 0, invalidRecord("quota observation filter limit must be between 1 and 500")
	}
	if filter.AccountScope != nil && *filter.AccountScope != QuotaAccountScopeDefault {
		return 0, invalidRecord("quota observation account scope filter is invalid")
	}
	if filter.Source != nil && !validQuotaSource(*filter.Source) {
		return 0, invalidRecord("quota observation source filter is invalid")
	}
	if filter.WindowKind != nil && !validQuotaWindowKind(*filter.WindowKind) {
		return 0, invalidRecord("quota observation window filter is invalid")
	}
	if filter.Validity != nil && !validQuotaValidity(*filter.Validity) {
		return 0, invalidRecord("quota observation validity filter is invalid")
	}
	if filter.SessionID != nil && *filter.SessionID == "" {
		return 0, invalidRecord("quota observation session filter is invalid")
	}
	if filter.SourceFileID != nil && *filter.SourceFileID == "" {
		return 0, invalidRecord("quota observation source file filter is invalid")
	}
	return limit, nil
}

func validateQuotaObservationSample(sample QuotaObservationSample) error {
	if sample.ObservationID == "" || len(sample.ObservationID) > 512 ||
		sample.AccountScope != QuotaAccountScopeDefault || !validQuotaSource(sample.Source) ||
		!validQuotaWindowKind(sample.WindowKind) || math.IsNaN(sample.UsedPercent) ||
		math.IsInf(sample.UsedPercent, 0) || sample.UsedPercent < 0 || sample.UsedPercent > 100 ||
		sample.WindowMinutes <= 0 || sample.WindowMinutes > maxQuotaWindowMinutes ||
		sample.ResetsAtMS < 0 || sample.ObservedAtMS < 0 ||
		!validQuotaValidity(sample.Validity) || sample.SourceGeneration < 0 || sample.SourceOffset < 0 {
		return invalidRecord("quota observation sample is invalid")
	}
	if err := validateOptionalStrings(
		sample.LimitID, sample.PlanType, sample.RequestID, sample.SessionID, sample.SourceFileID,
	); err != nil {
		return err
	}
	if sample.PlanType != nil && !validQuotaPlanType(*sample.PlanType) {
		return invalidRecord("quota observation plan type is invalid")
	}
	if sample.Validity == QuotaValidityAccepted {
		if sample.RejectionReason != nil || sample.LimitID == nil ||
			sample.PlanType != nil && *sample.PlanType == "unknown" || sample.ResetsAtMS <= sample.ObservedAtMS {
			return invalidRecord("accepted quota observation is not trustworthy")
		}
	} else if sample.RejectionReason == nil || !validQuotaRejectionReason(*sample.RejectionReason) {
		return invalidRecord("non-accepted quota observation needs an allowlisted reason")
	}
	switch sample.Source {
	case QuotaSourceLocalJSONL:
		if sample.SessionID == nil || sample.SourceFileID == nil || sample.RequestID != nil {
			return invalidRecord("local quota observation provenance is invalid")
		}
	case QuotaSourceWham:
		if sample.RequestID == nil || sample.SourceFileID != nil {
			return invalidRecord("online quota observation request is missing")
		}
	}
	return nil
}

func upsertQuotaObservation(ctx context.Context, transaction *gorm.DB, sample QuotaObservationSample) error {
	database := transaction.WithContext(ctx)
	sampleSHA256, err := quotaObservationSampleSHA256(sample)
	if err != nil {
		return err
	}
	var receipt quotaObservationReceiptModel
	err = database.Take(&receipt, "observation_id = ?", sample.ObservationID).Error
	if err == nil {
		if receipt.SampleSHA256 != sampleSHA256 {
			return invalidRecord("quota observation ID conflicts with stored sample")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	latest, found, err := latestQuotaObservationSegment(ctx, database, sample)
	if err != nil {
		return err
	}
	if found && latest.SourceGeneration == sample.SourceGeneration {
		if sample.SourceOffset <= latest.SourceOffset {
			if quotaObservationSemanticMatches(latest, sample) &&
				sample.ObservedAtMS >= latest.FirstObservedAtMS && sample.ObservedAtMS <= latest.LastObservedAtMS {
				return nil
			}
			return invalidRecord("quota observation source position does not advance")
		}
		if sample.ObservedAtMS >= latest.LastObservedAtMS && quotaObservationSemanticMatches(latest, sample) {
			if err := database.Model(&quotaObservationModel{}).
				Where("observation_id = ? AND source_generation = ? AND source_offset = ?", latest.ObservationID, latest.SourceGeneration, latest.SourceOffset).
				Updates(map[string]any{
					"last_observed_at_ms": sample.ObservedAtMS,
					"sample_count":        latest.SampleCount + 1,
					"request_id":          sample.RequestID,
					"source_offset":       sample.SourceOffset,
				}).Error; err != nil {
				return err
			}
			return createQuotaObservationReceipt(database, sample.ObservationID, latest.ObservationID, sampleSHA256)
		}
	}
	if err := database.Create(quotaObservationModelFromSample(sample)).Error; err != nil {
		return err
	}
	return createQuotaObservationReceipt(database, sample.ObservationID, sample.ObservationID, sampleSHA256)
}

func quotaObservationSampleSHA256(sample QuotaObservationSample) (string, error) {
	encoded, err := json.Marshal(sample)
	if err != nil {
		return "", invalidRecord("marshal quota observation replay receipt")
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func createQuotaObservationReceipt(
	database *gorm.DB,
	observationID string,
	segmentObservationID string,
	sampleSHA256 string,
) error {
	return database.Create(&quotaObservationReceiptModel{
		ObservationID: observationID, SegmentObservationID: segmentObservationID, SampleSHA256: sampleSHA256,
	}).Error
}

func latestQuotaObservationSegment(
	ctx context.Context,
	database *gorm.DB,
	sample QuotaObservationSample,
) (quotaObservationModel, bool, error) {
	query := database.WithContext(ctx).Where(
		"account_scope = ? AND source = ? AND window_kind = ? AND source_generation = ?",
		sample.AccountScope, string(sample.Source), string(sample.WindowKind), sample.SourceGeneration,
	)
	if sample.SourceFileID == nil {
		query = query.Where("source_file_id IS NULL")
	} else {
		query = query.Where("source_file_id = ?", *sample.SourceFileID)
	}
	if sample.SessionID == nil {
		query = query.Where("session_id IS NULL")
	} else {
		query = query.Where("session_id = ?", *sample.SessionID)
	}
	if sample.RequestID == nil {
		query = query.Where("request_id IS NULL")
	} else {
		query = query.Where("request_id = ?", *sample.RequestID)
	}
	if sample.LimitID == nil {
		query = query.Where("limit_id IS NULL")
	} else {
		query = query.Where("limit_id = ?", *sample.LimitID)
	}
	var model quotaObservationModel
	err := query.Order("source_offset DESC").Order("observation_id DESC").Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return quotaObservationModel{}, false, nil
	}
	return model, err == nil, err
}

func quotaObservationSemanticMatches(model quotaObservationModel, sample QuotaObservationSample) bool {
	reason := optionalQuotaReasonString(sample.RejectionReason)
	return model.AccountScope == sample.AccountScope && model.Source == string(sample.Source) &&
		equalStringPointer(model.LimitID, sample.LimitID) && model.WindowKind == string(sample.WindowKind) &&
		model.UsedPercent == sample.UsedPercent && model.WindowMinutes == sample.WindowMinutes &&
		model.ResetsAtMS == sample.ResetsAtMS && equalStringPointer(model.PlanType, sample.PlanType) &&
		model.Validity == string(sample.Validity) && equalStringPointer(model.RejectionReason, reason)
}

func quotaObservationModelFromSample(sample QuotaObservationSample) *quotaObservationModel {
	return &quotaObservationModel{
		ObservationID: sample.ObservationID, AccountScope: sample.AccountScope,
		Source: string(sample.Source), LimitID: cloneQuotaString(sample.LimitID), WindowKind: string(sample.WindowKind),
		UsedPercent: sample.UsedPercent, WindowMinutes: sample.WindowMinutes, ResetsAtMS: sample.ResetsAtMS,
		PlanType: cloneQuotaString(sample.PlanType), Validity: string(sample.Validity),
		RejectionReason:   optionalQuotaReasonString(sample.RejectionReason),
		FirstObservedAtMS: sample.ObservedAtMS, LastObservedAtMS: sample.ObservedAtMS, SampleCount: 1,
		RequestID: cloneQuotaString(sample.RequestID), SessionID: cloneQuotaString(sample.SessionID),
		SourceFileID:          cloneQuotaString(sample.SourceFileID),
		FirstSourceGeneration: sample.SourceGeneration, FirstSourceOffset: sample.SourceOffset,
		SourceGeneration: sample.SourceGeneration, SourceOffset: sample.SourceOffset,
	}
}

func quotaObservationFromModel(model quotaObservationModel) (QuotaObservation, error) {
	source, window, validity := QuotaSource(model.Source), QuotaWindowKind(model.WindowKind), QuotaValidity(model.Validity)
	if !validQuotaSource(source) || !validQuotaWindowKind(window) || !validQuotaValidity(validity) ||
		model.AccountScope != QuotaAccountScopeDefault || model.ObservationID == "" ||
		math.IsNaN(model.UsedPercent) || math.IsInf(model.UsedPercent, 0) ||
		model.UsedPercent < 0 || model.UsedPercent > 100 || model.WindowMinutes <= 0 ||
		model.WindowMinutes > maxQuotaWindowMinutes ||
		model.ResetsAtMS < 0 || model.FirstObservedAtMS < 0 ||
		model.LastObservedAtMS < model.FirstObservedAtMS || model.SampleCount <= 0 ||
		model.FirstSourceGeneration < 0 || model.SourceGeneration < model.FirstSourceGeneration ||
		model.FirstSourceOffset < 0 || model.SourceOffset < 0 {
		return QuotaObservation{}, invalidRecord("stored quota observation is invalid")
	}
	var reason *QuotaRejectionReason
	if model.RejectionReason != nil {
		parsed := QuotaRejectionReason(*model.RejectionReason)
		if !validQuotaRejectionReason(parsed) {
			return QuotaObservation{}, invalidRecord("stored quota observation reason is invalid")
		}
		reason = &parsed
	}
	if validity == QuotaValidityAccepted && (reason != nil || model.LimitID == nil ||
		model.PlanType != nil && *model.PlanType == "unknown" || model.ResetsAtMS <= model.LastObservedAtMS) ||
		validity != QuotaValidityAccepted && reason == nil {
		return QuotaObservation{}, invalidRecord("stored quota observation trust state is invalid")
	}
	if source == QuotaSourceLocalJSONL && (model.SourceFileID == nil || model.RequestID != nil) ||
		source == QuotaSourceWham && (model.SourceFileID != nil || model.RequestID == nil) {
		return QuotaObservation{}, invalidRecord("stored quota observation provenance is invalid")
	}
	return QuotaObservation{
		ObservationID: model.ObservationID, AccountScope: model.AccountScope, Source: source,
		LimitID: cloneQuotaString(model.LimitID), WindowKind: window, UsedPercent: model.UsedPercent,
		WindowMinutes: model.WindowMinutes, ResetsAtMS: model.ResetsAtMS, PlanType: cloneQuotaString(model.PlanType),
		Validity: validity, RejectionReason: reason, FirstObservedAtMS: model.FirstObservedAtMS,
		LastObservedAtMS: model.LastObservedAtMS, SampleCount: model.SampleCount,
		RequestID: cloneQuotaString(model.RequestID), SessionID: cloneQuotaString(model.SessionID),
		SourceFileID:          cloneQuotaString(model.SourceFileID),
		FirstSourceGeneration: model.FirstSourceGeneration, FirstSourceOffset: model.FirstSourceOffset,
		SourceGeneration: model.SourceGeneration, SourceOffset: model.SourceOffset,
	}, nil
}

func optionalQuotaReasonString(reason *QuotaRejectionReason) *string {
	if reason == nil {
		return nil
	}
	value := string(*reason)
	return &value
}

func cloneQuotaString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validQuotaSource(source QuotaSource) bool {
	return source == QuotaSourceLocalJSONL || source == QuotaSourceWham
}

func validQuotaWindowKind(kind QuotaWindowKind) bool {
	value := string(kind)
	return kind == QuotaWindowPrimary || kind == QuotaWindowSecondary ||
		strings.HasPrefix(value, "additional:") && len(value) > len("additional:")
}

func validQuotaValidity(validity QuotaValidity) bool {
	return validity == QuotaValidityAccepted || validity == QuotaValiditySuspicious || validity == QuotaValidityRejected
}

func validQuotaPlanType(plan string) bool {
	switch plan {
	case "free", "go", "plus", "pro", "prolite", "team", "self_serve_business_usage_based",
		"business", "enterprise_cbp_usage_based", "enterprise", "edu", "unknown":
		return true
	default:
		return false
	}
}

func validQuotaRejectionReason(reason QuotaRejectionReason) bool {
	switch reason {
	case QuotaReasonMissingLimitID, QuotaReasonMissingPrimaryWindow, QuotaReasonResetNotFuture,
		QuotaReasonUnknownPlanType, QuotaReasonInvalidUsedPercent, QuotaReasonInvalidWindowMinutes,
		QuotaReasonInvalidResetsAt, QuotaReasonInvalidStructure, QuotaReasonUsedRegression,
		QuotaReasonResetRegression, QuotaReasonObservedRegression, QuotaReasonSourceConflict,
		QuotaReasonDefaultFallback:
		return true
	default:
		return false
	}
}
