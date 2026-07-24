package attribution

import (
	"regexp"
	"strings"
)

const maxModelNameBytes = 128

var safeModelName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

var modelDisplayNames = map[string]string{
	"gpt-5-codex":         "GPT-5 Codex",
	"gpt-5.1-codex":       "GPT-5.1 Codex",
	"gpt-5.1-codex-max":   "GPT-5.1 Codex Max",
	"gpt-5.2-codex":       "GPT-5.2 Codex",
	"gpt-5.2-codex-max":   "GPT-5.2 Codex Max",
	"gpt-5.3-codex":       "GPT-5.3 Codex",
	"gpt-5.3-codex-spark": "GPT-5.3 Codex Spark",
	"gpt-5.4":             "GPT-5.4",
	"gpt-5.4-mini":        "GPT-5.4 Mini",
	"gpt-5.5":             "GPT-5.5",
	"gpt-5.6":             "GPT-5.6",
	"gpt-5.6-sol":         "GPT-5.6 Sol",
	"gpt-5.6-terra":       "GPT-5.6 Terra",
	"gpt-5.6-luna":        "GPT-5.6 Luna",
}

type ModelDecision struct {
	Key         string
	DisplayName string
	Confidence  Confidence
	Source      Source
	Reason      Reason
}

func NormalizeModel(raw string) ModelDecision {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ModelDecision{
			Confidence: ConfidenceUnknown, Source: SourceMissing, Reason: ReasonMissing,
		}
	}
	if len(trimmed) > maxModelNameBytes {
		return invalidModelDecision()
	}

	normalized := strings.ToLower(trimmed)
	alias := normalized != trimmed
	for _, prefix := range []string{"openai/", "openai:", "codex/", "codex:"} {
		if strings.HasPrefix(normalized, prefix) {
			normalized = strings.TrimPrefix(normalized, prefix)
			alias = true
			break
		}
	}
	if !safeModelName.MatchString(normalized) {
		return invalidModelDecision()
	}

	display := modelDisplayNames[normalized]
	if display == "" {
		display = normalized
	}
	source := SourceModelCanonical
	if alias {
		source = SourceModelAlias
	}
	return ModelDecision{
		Key: normalized, DisplayName: display, Confidence: ConfidenceHigh,
		Source: source, Reason: ReasonObserved,
	}
}

func invalidModelDecision() ModelDecision {
	return ModelDecision{
		Confidence: ConfidenceUnknown, Source: SourceInvalidModel, Reason: ReasonInvalid,
	}
}
