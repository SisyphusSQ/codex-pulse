package agentprovider

import (
	"errors"
	"slices"
	"testing"
)

func TestNormalizeSupportsFiveProvidersAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{input: "", want: Codex},
		{input: "  ", want: Codex},
		{input: "codex", want: Codex},
		{input: "CODEX", want: Codex},
		{input: "cursor", want: Cursor},
		{input: "grok", want: Grok},
		{input: "dsh", want: DSH},
		{input: "agy", want: AGY},
		{input: "  AGY  ", want: AGY},
	}
	for _, tc := range cases {
		got, err := Normalize(tc.input)
		if err != nil {
			t.Fatalf("Normalize(%q) unexpected error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	for _, invalid := range []string{"antigravity", "claude", "all", "unknown"} {
		if got, err := Normalize(invalid); !errors.Is(err, ErrInvalidProvider) || got != "" {
			t.Fatalf("Normalize(%q) = (%q, %v), want (\"\", ErrInvalidProvider)", invalid, got, err)
		}
	}
}

func TestAGYContextDeclaresSourcesAndOmitsTools(t *testing.T) {
	t.Parallel()

	ctx := AGYContext()
	if ctx.EffectiveProvider != AGY {
		t.Fatalf("EffectiveProvider = %q, want %q", ctx.EffectiveProvider, AGY)
	}
	if slices.Contains(ctx.Capabilities, "tools") {
		t.Fatalf("AGYContext capabilities must not contain tools: %v", ctx.Capabilities)
	}
	for _, want := range []string{"account", "quota", "sessions", "projects", "models", "tokens", "estimated_cost"} {
		if !slices.Contains(ctx.Capabilities, want) {
			t.Fatalf("AGYContext missing capability %q: %v", want, ctx.Capabilities)
		}
	}
	if len(ctx.Sources) != 4 || len(ctx.Coverage) != 3 {
		t.Fatalf("AGYContext sources/coverage = %#v", ctx)
	}
}
