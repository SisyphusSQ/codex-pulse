package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorEnvelopeFromMapsStableFailureClasses(t *testing.T) {
	secretCause := errors.New("synthetic-private-cause-must-not-escape")
	tests := []struct {
		name       string
		err        error
		code       ErrorCode
		messageKey string
		field      string
		retryable  bool
	}{
		{name: "validation", err: NewValidationFailure("page.limit", secretCause), code: ErrorValidation, messageKey: "query.error.validation", field: "page.limit"},
		{name: "command source validation", err: NewValidationFailure("source", secretCause), code: ErrorValidation, messageKey: "query.error.validation", field: "source"},
		{name: "not found", err: NewNotFoundFailure(secretCause), code: ErrorNotFound, messageKey: "query.error.notFound"},
		{name: "partial", err: NewPartialFailure(secretCause), code: ErrorPartial, messageKey: "query.error.partial", retryable: true},
		{name: "unavailable", err: NewUnavailableFailure(secretCause), code: ErrorUnavailable, messageKey: "query.error.unavailable", retryable: true},
		{name: "cancelled", err: fmt.Errorf("wrapped cancellation: %w", context.Canceled), code: ErrorCancelled, messageKey: "query.error.cancelled"},
		{name: "deadline", err: fmt.Errorf("wrapped deadline: %w", context.DeadlineExceeded), code: ErrorDeadlineExceeded, messageKey: "query.error.deadlineExceeded", retryable: true},
		{name: "unknown internal", err: secretCause, code: ErrorInternal, messageKey: "query.error.internal"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope, ok := ErrorEnvelopeFrom(test.err)
			if !ok {
				t.Fatal("ErrorEnvelopeFrom() ok = false")
			}
			if envelope.Version != ContractVersion || envelope.Error.Code != test.code ||
				envelope.Error.MessageKey != test.messageKey || envelope.Error.Retryable != test.retryable {
				t.Fatalf("envelope = %#v", envelope)
			}
			if test.field == "" {
				if envelope.Error.Field != nil {
					t.Fatalf("field = %q, want nil", *envelope.Error.Field)
				}
			} else if envelope.Error.Field == nil || *envelope.Error.Field != test.field {
				t.Fatalf("field = %#v, want %q", envelope.Error.Field, test.field)
			}
			encoded, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if strings.Contains(string(encoded), secretCause.Error()) || strings.Contains(test.err.Error(), secretCause.Error()) && test.code != ErrorInternal && !errors.Is(test.err, context.Canceled) && !errors.Is(test.err, context.DeadlineExceeded) {
				t.Fatalf("private cause escaped: error=%q json=%s", test.err, encoded)
			}
		})
	}

	if envelope, ok := ErrorEnvelopeFrom(nil); ok || envelope != (ErrorEnvelope{}) {
		t.Fatalf("ErrorEnvelopeFrom(nil) = %#v, %v", envelope, ok)
	}
}

func TestFailurePreservesInternalErrorChainWithoutExposingCause(t *testing.T) {
	cause := errors.New("synthetic-secret-cause")
	err := NewUnavailableFailure(cause)
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("errors.Is() failed for %v", err)
	}
	var failure *Failure
	if !errors.As(err, &failure) || failure.Field() != "" {
		t.Fatalf("errors.As() = %#v", failure)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("Error() exposed cause: %q", err)
	}

	validation := NewValidationFailure("filters.field", cause)
	if !errors.Is(validation, ErrValidation) || !errors.Is(validation, cause) {
		t.Fatalf("validation chain = %v", validation)
	}
	if !errors.As(validation, &failure) || failure.Field() != "filters.field" {
		t.Fatalf("validation failure = %#v", failure)
	}
}

func TestNewValidationFailureRejectsUnregisteredFieldName(t *testing.T) {
	for _, field := range []string{"page.limit;DROP TABLE", "authToken"} {
		err := NewValidationFailure(field, errors.New("synthetic"))
		if !errors.Is(err, ErrInvalidSpecification) {
			t.Fatalf("NewValidationFailure(%q) error = %v, want ErrInvalidSpecification", field, err)
		}
	}
}
