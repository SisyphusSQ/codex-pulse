package runtimeinfo

import (
	"strings"
	"unicode"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func knownNumeric(value int64, unit basequery.NumericUnit) (basequery.NumericValue, error) {
	return basequery.KnownNumeric(value, unit)
}

func unknownNumeric(
	unit basequery.NumericUnit,
	reason basequery.UnknownReason,
) (basequery.NumericValue, error) {
	return basequery.UnknownNumeric(unit, reason)
}

func optionalNumeric(
	value *int64,
	unit basequery.NumericUnit,
	reason basequery.UnknownReason,
) (basequery.NumericValue, error) {
	if value == nil {
		return unknownNumeric(unit, reason)
	}
	return knownNumeric(*value, unit)
}

func responseCompleteness(partial bool) (basequery.ResponseStatus, []basequery.ErrorCode) {
	if partial {
		return basequery.ResponsePartial, []basequery.ErrorCode{basequery.ErrorPartial}
	}
	return basequery.ResponseComplete, nil
}

func noRecovery() RecoveryAction {
	return RecoveryAction{Kind: RecoveryNone}
}

func recovery(kind RecoveryActionKind, command string) RecoveryAction {
	if kind == RecoveryNone || command == "" {
		return noRecovery()
	}
	return RecoveryAction{Kind: kind, CommandKey: cloneString(command)}
}

func sourceRecovery(
	errorClass *store.RuntimeErrorClass,
	failureCode *store.SourceFailureCode,
	attention bool,
) RecoveryAction {
	if failureCode != nil {
		switch *failureCode {
		case store.SourceFailureAuthRequired:
			return recovery(RecoveryGrantPermission, CommandGrantPermission)
		case store.SourceFailureNetworkUnavailable, store.SourceFailureTimeout,
			store.SourceFailureHTTP429, store.SourceFailureServerError:
			return recovery(RecoveryRetry, CommandRetrySource)
		case store.SourceFailureSchemaIncompatible:
			return recovery(RecoveryCheckSource, CommandCheckSource)
		case store.SourceFailureCancelled:
			return noRecovery()
		}
	}
	if errorClass != nil {
		if *errorClass == store.RuntimeErrorCanceled || *errorClass == store.RuntimeErrorInvalid {
			return noRecovery()
		}
		if action := recoveryForError(errorClass, CommandRetrySource); action.Kind != RecoveryNone {
			return action
		}
	}
	if attention {
		return recovery(RecoveryCheckSource, CommandCheckSource)
	}
	return noRecovery()
}

func jobRecovery(record store.RuntimeJobRecord) RecoveryAction {
	if record.Retry != nil {
		return recoveryForStoredAction(record.Retry.RecoveryAction, CommandRetryJob)
	}
	if action := recoveryForError(record.Job.ErrorClass, CommandRetryJob); action.Kind != RecoveryNone {
		return action
	}
	if record.Job.State == store.JobFailed || record.Job.State == store.JobInterrupted {
		return recovery(RecoveryRetry, CommandRetryJob)
	}
	return noRecovery()
}

func healthRecovery(event store.HealthEvent) RecoveryAction {
	if event.ResolvedAtMS != nil {
		return noRecovery()
	}
	switch event.Code {
	case store.HealthCodeSourceTimeout:
		return recovery(RecoveryRetry, CommandRetryHealth)
	case store.HealthCodeSourceUnavailable, store.HealthCodeSourceStale:
		return recovery(RecoveryCheckSource, CommandCheckSource)
	case store.HealthCodeSourcePermission:
		return recovery(RecoveryGrantPermission, CommandGrantPermission)
	case store.HealthCodeSourceCorrupt:
		return recovery(RecoveryRepairStore, CommandRepairStore)
	case store.HealthCodeJobInterrupted, store.HealthCodeJobFailed:
		return recovery(RecoveryRetry, CommandRetryHealth)
	case store.HealthCodeJobCancelled:
		return noRecovery()
	case store.HealthCodeStoreBusy, store.HealthCodeStoreIO,
		store.HealthCodeStoreUnavailable, store.HealthCodeStoreUnknown:
		return recovery(RecoveryRetry, CommandRetryHealth)
	case store.HealthCodeStoreDiskFull:
		return recovery(RecoveryFreeSpace, CommandFreeSpace)
	case store.HealthCodeStoreReadOnly, store.HealthCodeStorePermission:
		return recovery(RecoveryGrantPermission, CommandGrantPermission)
	case store.HealthCodeStoreCorrupt, store.HealthCodePricingInvalid:
		return recovery(RecoveryRepairStore, CommandRepairStore)
	case store.HealthCodePricingUnavailable, store.HealthCodeRuntimeUnknown:
		return recovery(RecoveryRetry, CommandRetryHealth)
	}
	return recoveryForError(event.ErrorClass, CommandRetryHealth)
}

func recoveryForStoredAction(
	action store.SchedulerRecoveryAction,
	retryCommand string,
) RecoveryAction {
	switch action {
	case store.SchedulerRecoveryNone:
		return noRecovery()
	case store.SchedulerRecoveryRetry:
		return recovery(RecoveryRetry, retryCommand)
	case store.SchedulerRecoveryCheckSource:
		return recovery(RecoveryCheckSource, CommandCheckSource)
	case store.SchedulerRecoveryGrantPermission:
		return recovery(RecoveryGrantPermission, CommandGrantPermission)
	case store.SchedulerRecoveryFreeSpace:
		return recovery(RecoveryFreeSpace, CommandFreeSpace)
	case store.SchedulerRecoveryChooseHome:
		return recovery(RecoveryChooseHome, CommandChooseHome)
	case store.SchedulerRecoveryRepairStore:
		return recovery(RecoveryRepairStore, CommandRepairStore)
	default:
		return noRecovery()
	}
}

func recoveryForError(errorClass *store.RuntimeErrorClass, retryCommand string) RecoveryAction {
	if errorClass == nil {
		return noRecovery()
	}
	switch *errorClass {
	case store.RuntimeErrorPermission:
		return recovery(RecoveryGrantPermission, CommandGrantPermission)
	case store.RuntimeErrorDiskFull:
		return recovery(RecoveryFreeSpace, CommandFreeSpace)
	case store.RuntimeErrorCorrupt:
		return recovery(RecoveryRepairStore, CommandRepairStore)
	case store.RuntimeErrorReadOnly:
		return recovery(RecoveryGrantPermission, CommandGrantPermission)
	case store.RuntimeErrorBusy, store.RuntimeErrorIO, store.RuntimeErrorTimeout,
		store.RuntimeErrorUnavailable, store.RuntimeErrorUnknown:
		return recovery(RecoveryRetry, retryCommand)
	case store.RuntimeErrorCanceled, store.RuntimeErrorInvalid:
		return noRecovery()
	default:
		return noRecovery()
	}
}

func runtimeErrorString(value *store.RuntimeErrorClass) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func sourceFailureString(value *store.SourceFailureCode) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func validRuntimeErrorPointer(value *store.RuntimeErrorClass) bool {
	return value == nil || validRuntimeErrorClass(*value)
}

func validRuntimeErrorClass(value store.RuntimeErrorClass) bool {
	switch value {
	case store.RuntimeErrorCanceled, store.RuntimeErrorBusy, store.RuntimeErrorDiskFull,
		store.RuntimeErrorReadOnly, store.RuntimeErrorPermission, store.RuntimeErrorIO,
		store.RuntimeErrorCorrupt, store.RuntimeErrorTimeout, store.RuntimeErrorUnavailable,
		store.RuntimeErrorInvalid, store.RuntimeErrorUnknown:
		return true
	default:
		return false
	}
}

func validSourceFailurePointer(value *store.SourceFailureCode) bool {
	if value == nil {
		return true
	}
	switch *value {
	case store.SourceFailureNetworkUnavailable, store.SourceFailureTimeout,
		store.SourceFailureAuthRequired, store.SourceFailureHTTP429,
		store.SourceFailureServerError, store.SourceFailureSchemaIncompatible,
		store.SourceFailureCancelled:
		return true
	default:
		return false
	}
}

func validStoredRecoveryAction(value store.SchedulerRecoveryAction) bool {
	switch value {
	case store.SchedulerRecoveryNone, store.SchedulerRecoveryRetry,
		store.SchedulerRecoveryCheckSource, store.SchedulerRecoveryGrantPermission,
		store.SchedulerRecoveryFreeSpace, store.SchedulerRecoveryChooseHome,
		store.SchedulerRecoveryRepairStore:
		return true
	default:
		return false
	}
}

func validRetryDisposition(value store.SchedulerRetryDisposition) bool {
	return value == store.SchedulerRetryWaiting || value == store.SchedulerRetryBlocked ||
		value == store.SchedulerRetryResolved
}

func validRetryState(value store.SchedulerRetryState) bool {
	if value.FailureCount < 1 || !validOpaqueIdentity(value.TaskID) || value.Revision < 1 ||
		value.UpdatedAtMS < 0 || !validRetryDisposition(value.Disposition) ||
		!validStoredRecoveryAction(value.RecoveryAction) ||
		!validRuntimeErrorClass(value.LastErrorClass) ||
		value.NextRetryAtMS != nil && *value.NextRetryAtMS < 0 {
		return false
	}
	switch value.Disposition {
	case store.SchedulerRetryWaiting:
		return value.NextRetryAtMS != nil && *value.NextRetryAtMS > value.UpdatedAtMS &&
			value.RecoveryAction == store.SchedulerRecoveryNone
	case store.SchedulerRetryBlocked:
		return value.NextRetryAtMS == nil && value.RecoveryAction != store.SchedulerRecoveryNone
	case store.SchedulerRetryResolved:
		return value.NextRetryAtMS == nil && value.RecoveryAction == store.SchedulerRecoveryNone
	default:
		return false
	}
}

func validPublicToken(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	for _, current := range value {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) &&
			!strings.ContainsRune("._:-", current) {
			return false
		}
	}
	return true
}

func validOpaqueIdentity(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, current := range value {
		if unicode.IsSpace(current) || unicode.IsControl(current) || current == '/' || current == '\\' {
			return false
		}
	}
	return true
}

func cloneString(value string) *string {
	cloned := value
	return &cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return cloneString(*value)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
