package jsonshape

import (
	"errors"
	"testing"
)

func TestValidateDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		duplicate bool
		valid     bool
	}{
		{name: "nested document", content: `{"root":[{"value":1},{"value":2.5}],"enabled":true}`, valid: true},
		{name: "top-level duplicate", content: `{"value":1,"value":2}`, duplicate: true},
		{name: "nested duplicate", content: `{"root":[{"value":1,"value":2}]}`, duplicate: true},
		{name: "multiple values", content: `{"value":1} {"value":2}`},
		{name: "unterminated object", content: `{"value":1`},
		{name: "unterminated array", content: `[1,2`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDocument([]byte(test.content))
			if test.valid && err != nil {
				t.Fatalf("ValidateDocument() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("ValidateDocument() error = nil")
			}
			if got := errors.Is(err, ErrDuplicateKey); got != test.duplicate {
				t.Fatalf("errors.Is(ErrDuplicateKey) = %t, want %t; error = %v", got, test.duplicate, err)
			}
		})
	}
}
