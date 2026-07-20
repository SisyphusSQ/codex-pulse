// Package privacy defines the content-free audit contract shared by validation
// runners. It never receives a real credential or user conversation as an
// audit input; callers use synthetic canaries and report only Finding codes.
package privacy

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"unicode"
)

const ContractVersion = "m11-privacy-v1"

type Finding string

const (
	FindingForbiddenField Finding = "forbidden_field"
	FindingSensitiveValue Finding = "sensitive_value"
	FindingAbsolutePath   Finding = "absolute_path"
	FindingInvalidValue   Finding = "invalid_value"
)

type Violation struct{ Finding Finding }

func (violation *Violation) Error() string {
	return "privacy contract violation: " + string(violation.Finding)
}

func IsFinding(err error, finding Finding) bool {
	var violation *Violation
	return errors.As(err, &violation) && violation.Finding == finding
}

var forbiddenFieldTokens = [][]string{
	{"tool", "output"}, {"raw", "json"},
	{"authorization"}, {"cookie"}, {"access", "token"}, {"refresh", "token"},
	{"auth"}, {"auth", "token"}, {"secret"}, {"token", "secret"}, {"credential"},
	{"raw", "error"}, {"error", "message"}, {"stack", "trace"},
}

var safeFieldTokens = [][]string{
	{"prompt", "visible"}, // native confirmation UI state; contains no prompt body
	{"prompt", "tokens"},
	{"total", "prompt", "tokens"},
	{"response", "bytes"},
	{"content", "length"},
}

// InspectField rejects dedicated storage or public-contract fields for raw
// content, credentials and unclassified errors. Numeric fields such as
// responseBytes and contentLength remain valid metadata.
func InspectField(name string) error {
	tokens := fieldTokens(name)
	for _, safe := range safeFieldTokens {
		if equalExactTokens(tokens, safe) {
			return nil
		}
	}
	for _, forbidden := range forbiddenFieldTokens {
		if containsTokens(tokens, forbidden) {
			return &Violation{Finding: FindingForbiddenField}
		}
	}
	if containsBodyToken(tokens) {
		return &Violation{Finding: FindingForbiddenField}
	}
	return nil
}

func containsBodyToken(tokens []string) bool {
	for _, token := range tokens {
		if token == "prompt" || token == "prompts" || token == "response" || token == "responses" || token == "content" || token == "contents" {
			return true
		}
	}
	return false
}

// InspectPublicValue recursively checks a value that can cross an RPC, log,
// health, cache or evidence boundary. It never includes the rejected value in
// the returned error.
func InspectPublicValue(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return &Violation{Finding: FindingInvalidValue}
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return &Violation{Finding: FindingInvalidValue}
	}
	return inspectPublic(decoded)
}

func inspectPublic(value any) error {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if filepath.IsAbs(trimmed) {
			return &Violation{Finding: FindingAbsolutePath}
		}
		if ContainsSensitiveEnvelope(trimmed) {
			return &Violation{Finding: FindingSensitiveValue}
		}
	case []any:
		for _, item := range typed {
			if err := inspectPublic(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if err := InspectField(key); err != nil {
				return err
			}
			if err := inspectPublic(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// ContainsSensitiveEnvelope detects credential/body envelopes, not arbitrary
// words such as tokenCount or responseBytes that are safe aggregate metadata.
func ContainsSensitiveEnvelope(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "authorization: bearer ") || strings.Contains(lower, "cookie:") {
		return true
	}
	compact := strings.Map(func(value rune) rune {
		if unicode.IsSpace(value) {
			return -1
		}
		return value
	}, lower)
	for _, forbidden := range []string{
		`"authorization":`, `"cookie":`, `"access_token":`, `"refresh_token":`,
		`"auth":`, `"prompt":`, `"response":`, `"role":"user"`,
		`"type":"response_item"`, `"tool_output":`,
	} {
		if strings.Contains(compact, forbidden) {
			return true
		}
	}
	return false
}

// InspectArtifact rejects known synthetic canaries and sensitive envelopes.
// It deliberately does not print or return the matching bytes.
func InspectArtifact(content []byte, canaries ...string) error {
	text := string(content)
	if ContainsSensitiveEnvelope(text) {
		return &Violation{Finding: FindingSensitiveValue}
	}
	for _, canary := range canaries {
		if canary != "" && strings.Contains(text, canary) {
			return &Violation{Finding: FindingSensitiveValue}
		}
	}
	return nil
}

func fieldTokens(value string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, strings.ToLower(string(current)))
			current = nil
		}
	}
	var previousLower bool
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			previousLower = false
			continue
		}
		if unicode.IsUpper(character) && previousLower {
			flush()
		}
		current = append(current, character)
		previousLower = unicode.IsLower(character) || unicode.IsDigit(character)
	}
	flush()
	return tokens
}

func equalTokens(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] && left[index] != right[index]+"s" {
			return false
		}
	}
	return true
}

func equalExactTokens(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsTokens(tokens, sequence []string) bool {
	if len(sequence) == 0 || len(sequence) > len(tokens) {
		return false
	}
	for start := 0; start <= len(tokens)-len(sequence); start++ {
		if equalTokens(tokens[start:start+len(sequence)], sequence) {
			return true
		}
	}
	return false
}
