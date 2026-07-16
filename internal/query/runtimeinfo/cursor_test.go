package runtimeinfo

import (
	"errors"
	"testing"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func TestRuntimeCursorRoundTripRejectsTamperAndCrossEndpoint(t *testing.T) {
	t.Parallel()

	encoded, err := encodeCursor(
		sourceCursorEndpoint, "updatedAt", basequery.SortDescending, 100, "online:quota-a",
	)
	if err != nil || encoded == nil {
		t.Fatalf("encodeCursor() = %v, %v", encoded, err)
	}
	value, identity, err := decodeCursor(
		*encoded, sourceCursorEndpoint, "updatedAt", basequery.SortDescending,
	)
	if err != nil || value != 100 || identity != "online:quota-a" {
		t.Fatalf("decodeCursor() = %d, %q, %v", value, identity, err)
	}
	if _, _, err := decodeCursor(
		*encoded, jobCursorEndpoint, "updatedAt", basequery.SortDescending,
	); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("decodeCursor(cross endpoint) error = %v, want validation", err)
	}
	tampered := []byte(*encoded)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	if _, _, err := decodeCursor(
		string(tampered), sourceCursorEndpoint, "updatedAt", basequery.SortDescending,
	); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("decodeCursor(tampered) error = %v, want validation", err)
	}
}
