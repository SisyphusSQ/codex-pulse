package query

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrInvalidSpecification 表示 endpoint query specification 自身不完整或冲突。
	ErrInvalidSpecification = errors.New("query specification is invalid")
	// ErrValidation 表示调用方 request 未通过公共 query contract。
	ErrValidation = errors.New("query request is invalid")
	// ErrNotFound 表示稳定业务 identity 没有对应事实。
	ErrNotFound = errors.New("query result is not found")
	// ErrPartial 表示有可用事实，但目标范围不完整。
	ErrPartial = errors.New("query result is partial")
	// ErrUnavailable 表示当前无法读取权威事实。
	ErrUnavailable = errors.New("query result is unavailable")
)

// ErrorCode 是跨端有限错误分类。
type ErrorCode string

const (
	ErrorValidation       ErrorCode = "validation"
	ErrorNotFound         ErrorCode = "not_found"
	ErrorPartial          ErrorCode = "partial"
	ErrorUnavailable      ErrorCode = "unavailable"
	ErrorCancelled        ErrorCode = "cancelled"
	ErrorDeadlineExceeded ErrorCode = "deadline_exceeded"
	ErrorInternal         ErrorCode = "internal"
)

// ErrorDetail 只暴露稳定分类、i18n key、allowlisted field 和 retryable。
type ErrorDetail struct {
	Code       ErrorCode `json:"code"`
	MessageKey string    `json:"messageKey"`
	Field      *string   `json:"field"`
	Retryable  bool      `json:"retryable"`
}

// ErrorEnvelope 是 fatal query failure 的版本化跨端 envelope。
type ErrorEnvelope struct {
	Version string      `json:"version"`
	Error   ErrorDetail `json:"error"`
}

// Failure 保留内部 cause chain，但 Error 文本不包含 request value 或底层错误内容。
type Failure struct {
	category error
	field    string
	cause    error
}

// Error 返回固定分类文本，避免把底层 cause 暴露到跨端 surface。
func (failure *Failure) Error() string {
	if failure == nil || failure.category == nil {
		return "query failure"
	}
	return failure.category.Error()
}

// Unwrap 保留内部 errors.Is/errors.As 诊断链。
func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// Is 使固定分类在包装后仍可通过 errors.Is 判断。
func (failure *Failure) Is(target error) bool {
	if failure == nil {
		return false
	}
	return target == failure.category || errors.Is(failure.cause, target)
}

// Field 返回 allowlisted DTO 字段名，不返回用户输入值。
func (failure *Failure) Field() string {
	if failure == nil {
		return ""
	}
	return failure.field
}

func validationFailure(field string) error {
	return NewValidationFailure(field, nil)
}

// NewValidationFailure 创建带 allowlisted field 的 request validation failure。
func NewValidationFailure(field string, cause error) error {
	if !validFailureField(field) {
		return fmt.Errorf("%w: validation field", ErrInvalidSpecification)
	}
	return &Failure{category: ErrValidation, field: field, cause: cause}
}

// NewNotFoundFailure 包装 domain not-found cause。
func NewNotFoundFailure(cause error) error {
	return &Failure{category: ErrNotFound, cause: cause}
}

// NewPartialFailure 包装可返回部分数据的 cause。
func NewPartialFailure(cause error) error {
	return &Failure{category: ErrPartial, cause: cause}
}

// NewUnavailableFailure 包装当前不可用但可能恢复的 cause。
func NewUnavailableFailure(cause error) error {
	return &Failure{category: ErrUnavailable, cause: cause}
}

// ErrorEnvelopeFrom 把内部 error chain 映射为 content-free 跨端错误。
func ErrorEnvelopeFrom(err error) (ErrorEnvelope, bool) {
	if err == nil {
		return ErrorEnvelope{}, false
	}
	code := classifyErrorCode(err)
	detail := errorDetail(code)
	if code == ErrorValidation {
		var failure *Failure
		if errors.As(err, &failure) && failure.Field() != "" {
			field := failure.Field()
			detail.Field = &field
		}
	}
	return ErrorEnvelope{Version: ContractVersion, Error: detail}, true
}

func classifyErrorCode(err error) ErrorCode {
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorDeadlineExceeded
	case errors.Is(err, ErrValidation):
		return ErrorValidation
	case errors.Is(err, ErrNotFound):
		return ErrorNotFound
	case errors.Is(err, ErrPartial):
		return ErrorPartial
	case errors.Is(err, ErrUnavailable):
		return ErrorUnavailable
	default:
		return ErrorInternal
	}
}

func errorDetail(code ErrorCode) ErrorDetail {
	detail := ErrorDetail{Code: code}
	switch code {
	case ErrorValidation:
		detail.MessageKey = "query.error.validation"
	case ErrorNotFound:
		detail.MessageKey = "query.error.notFound"
	case ErrorPartial:
		detail.MessageKey = "query.error.partial"
		detail.Retryable = true
	case ErrorUnavailable:
		detail.MessageKey = "query.error.unavailable"
		detail.Retryable = true
	case ErrorCancelled:
		detail.MessageKey = "query.error.cancelled"
	case ErrorDeadlineExceeded:
		detail.MessageKey = "query.error.deadlineExceeded"
		detail.Retryable = true
	default:
		detail.Code = ErrorInternal
		detail.MessageKey = "query.error.internal"
	}
	return detail
}

func validFailureField(value string) bool {
	switch value {
	case "page.limit", "page.cursor",
		"turnPage.limit", "turnPage.cursor",
		"sessionPage.limit", "sessionPage.cursor",
		"modelPage.limit", "modelPage.cursor",
		"sort", "sort.field", "sort.direction",
		"filters", "filters.field", "filters.operator", "filters.values",
		"timeRange", "timeRange.timeZone", "timeRange.startDate", "timeRange.endDateExclusive",
		"numeric.unit", "numeric.value", "numeric.unknownReason",
		"response.status", "response.issues",
		"response.page.limit", "response.page.hasMore", "response.page.nextCursor",
		"sessionId", "reportingTimezone", "projectKey",
		"source", "sourceKey", "jobId", "eventId", "evaluatedAtMS",
		"settings", "targetPath", "strategy", "action":
		return true
	default:
		return false
	}
}
