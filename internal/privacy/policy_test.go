package privacy

import (
	"errors"
	"testing"
)

func TestInspectFieldSeparatesForbiddenBodiesFromSafeAggregateMetadata(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"prompt", "prompt_text", "responseBody", "tool_output_text", "authorization_header",
		"accessToken", "refresh_tokens", "auth", "token_secret_value", "credential", "rawErrorDetail",
		"error_message_text", "stack_trace_text", "content", "contents", "responseBodyBytes", "contentToken",
	} {
		if err := InspectField(field); !IsFinding(err, FindingForbiddenField) {
			t.Fatalf("InspectField(%q) error = %v", field, err)
		}
	}
	for _, field := range []string{"promptTokens", "totalPromptTokens", "promptVisible", "responseBytes", "contentLength", "errorClass", "errorCode", "tokenCount"} {
		if err := InspectField(field); err != nil {
			t.Fatalf("InspectField(%q) error = %v", field, err)
		}
	}
}

func TestInspectPublicValueRejectsPathsBodiesAndCredentialsWithoutEcho(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value   any
		finding Finding
	}{
		{value: map[string]any{"path": "/private/synthetic/project"}, finding: FindingAbsolutePath},
		{value: map[string]any{"safe": `{"prompt":"synthetic body"}`}, finding: FindingSensitiveValue},
		{value: map[string]any{"accessToken": "synthetic"}, finding: FindingForbiddenField},
	}
	for _, test := range tests {
		err := InspectPublicValue(test.value)
		if !IsFinding(err, test.finding) {
			t.Fatalf("InspectPublicValue() error = %v, want %s", err, test.finding)
		}
		if errors.Is(err, errors.New("synthetic")) || (err != nil && len(err.Error()) > 80) {
			t.Fatalf("violation exposed input: %v", err)
		}
	}
	if err := InspectPublicValue(map[string]any{"responseBytes": 42, "errorClass": "timeout"}); err != nil {
		t.Fatalf("safe metadata error = %v", err)
	}
}

func TestInspectArtifactUsesOnlySyntheticCanaries(t *testing.T) {
	t.Parallel()
	const canary = "M11_E4_SYNTHETIC_CANARY"
	if err := InspectArtifact([]byte("safe aggregate metadata"), canary); err != nil {
		t.Fatalf("InspectArtifact(safe) error = %v", err)
	}
	if err := InspectArtifact([]byte("prefix "+canary+" suffix"), canary); !IsFinding(err, FindingSensitiveValue) {
		t.Fatalf("InspectArtifact(canary) error = %v", err)
	}
	if err := InspectArtifact([]byte("Authorization: Bearer synthetic")); !IsFinding(err, FindingSensitiveValue) {
		t.Fatalf("InspectArtifact(header) error = %v", err)
	}
}
