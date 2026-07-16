package app

import (
	"encoding/json"
	"errors"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type bindingFailure struct {
	envelope basequery.ErrorEnvelope
	cause    error
}

func newBindingFailure(cause error) error {
	if cause == nil {
		return nil
	}
	envelope, _ := basequery.ErrorEnvelopeFrom(cause)
	return &bindingFailure{envelope: envelope, cause: cause}
}

func (*bindingFailure) Error() string {
	return ErrBindingQuery.Error()
}

func (failure *bindingFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func marshalBindingError(err error) []byte {
	var failure *bindingFailure
	var envelope basequery.ErrorEnvelope
	if errors.As(err, &failure) {
		envelope = failure.envelope
	} else {
		envelope, _ = basequery.ErrorEnvelopeFrom(err)
	}
	content, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		return []byte(`{"version":"query-v1","error":{"code":"internal","messageKey":"query.error.internal","field":null,"retryable":false}}`)
	}
	return content
}
