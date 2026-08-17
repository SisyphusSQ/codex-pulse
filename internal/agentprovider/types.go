// Package agentprovider defines the product-level client boundary. Model vendors
// such as OpenAI and Anthropic deliberately do not belong to this namespace.
package agentprovider

import (
	"errors"
	"strings"
)

const (
	Codex  = "codex"
	Cursor = "cursor"
)

var ErrInvalidProvider = errors.New("agent provider is invalid")

type Scope struct {
	Provider string `json:"provider"`
}

func Normalize(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", Codex:
		return Codex, nil
	case Cursor:
		return Cursor, nil
	default:
		return "", ErrInvalidProvider
	}
}

type Coverage struct {
	Capability string `json:"capability"`
	State      string `json:"state"`
	Source     string `json:"source"`
	Reason     string `json:"reason"`
	ItemCount  *int64 `json:"itemCount"`
}

type Context struct {
	EffectiveProvider string     `json:"effectiveProvider"`
	Sources           []string   `json:"sources"`
	Capabilities      []string   `json:"capabilities"`
	Coverage          []Coverage `json:"coverage"`
}

func CodexContext() Context {
	return Context{
		EffectiveProvider: Codex,
		Sources:           []string{"codex_local_jsonl", "codex_app_server"},
		Capabilities: []string{
			"account", "quota", "sessions", "projects", "models", "tools", "tokens", "estimated_cost",
		},
		Coverage: []Coverage{
			{Capability: "sessions", State: "available", Source: "codex_local_jsonl", Reason: "structured_events"},
			{Capability: "tokens", State: "available", Source: "codex_local_jsonl", Reason: "usage_events"},
			{Capability: "quota", State: "available", Source: "codex_app_server", Reason: "provider_quota"},
		},
	}
}
