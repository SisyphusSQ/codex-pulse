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
	Grok   = "grok"
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
	case Grok:
		return Grok, nil
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

func GrokContext() Context {
	return Context{
		EffectiveProvider: Grok,
		Sources:           []string{"grok.summary", "grok.updates", "grok.billing"},
		Capabilities: []string{
			"account", "quota", "sessions", "projects", "models", "tools", "tokens", "reported_cost", "estimated_cost",
		},
		Coverage: []Coverage{
			{Capability: "sessions", State: "available", Source: "grok.summary", Reason: "summary_json"},
			{Capability: "tokens", State: "available", Source: "grok.updates", Reason: "turn_completed_usage"},
			{Capability: "quota", State: "available", Source: "grok.billing", Reason: "cli_proxy_credits"},
		},
	}
}
