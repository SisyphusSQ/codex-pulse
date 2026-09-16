package subscriptionaccounts

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestParsePublicIDRejectsNonCanonicalValues(t *testing.T) {
	t.Parallel()
	valid := uuid.NewString()
	parsed, err := ParsePublicID(valid)
	if err != nil || parsed != valid {
		t.Fatalf("ParsePublicID(canonical) = %q, %v", parsed, err)
	}
	for name, value := range map[string]string{
		"uppercase":      strings.ToUpper(valid),
		"urn":            "urn:uuid:" + valid,
		"missing hyphen": strings.ReplaceAll(valid, "-", ""),
		"nil":            uuid.Nil.String(),
		"empty":          "",
		"short":          valid[:35],
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePublicID(value)
			if err != ErrInvalidUUID {
				t.Fatalf("ParsePublicID(%s) error = %v", name, err)
			}
			if strings.Contains(err.Error(), value) && value != "" {
				t.Fatalf("error leaked uuid: %v", err)
			}
		})
	}
}

func TestNormalizeEmailRejectsInvalidShapes(t *testing.T) {
	t.Parallel()
	tooLong := strings.Repeat("a", maxEmailBytes-6) + "@ex.com"
	invalidUTF8 := string([]byte{'a', 0xff, '@', 'b', '.', 'c'})
	for name, value := range map[string]string{
		"empty":          "",
		"spaces":         "   ",
		"missing at":     "user.example.com",
		"two at":         "a@b@c.com",
		"leading at":     "@example.com",
		"trailing at":    "user@",
		"internal space": "user @example.com",
		"tab":            "user@exam ple.com",
		"control":        "user\x00@example.com",
		"newline":        "user@exam\nple.com",
		"too long":       tooLong,
		"invalid utf8":   invalidUTF8,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := NormalizeEmail(value)
			if err != ErrInvalidEmail {
				t.Fatalf("NormalizeEmail(%s) error = %v", name, err)
			}
			if trimmed := strings.TrimSpace(value); trimmed != "" && strings.Contains(err.Error(), trimmed) {
				t.Fatalf("error leaked email: %v", err)
			}
		})
	}
}

func TestNormalizeEmailKeepsDisplayAndLowercasesMatchKey(t *testing.T) {
	t.Parallel()
	display, key, err := NormalizeEmail("  User@Example.COM  ")
	if err != nil {
		t.Fatal(err)
	}
	if display != "User@Example.COM" || key != "user@example.com" {
		t.Fatalf("NormalizeEmail() = %q, %q", display, key)
	}
	if got, want := len(display), utf8.RuneCountInString(display); got < want {
		t.Fatalf("display is not valid bytes")
	}
}

func TestOptionalEmailClearsBlankAndKeepsValid(t *testing.T) {
	t.Parallel()
	display, key, err := OptionalEmail(pointer("  "))
	if err != nil || display != nil || key != nil {
		t.Fatalf("OptionalEmail(blank) = %v %v %v", display, key, err)
	}
	display, key, err = OptionalEmail(pointer("A@B.C"))
	if err != nil || display == nil || key == nil || *display != "A@B.C" || *key != "a@b.c" {
		t.Fatalf("OptionalEmail(valid) = %v %v %v", display, key, err)
	}
}

func TestOptionalAliasRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	tooLong := strings.Repeat("名", maxAliasBytes+1)
	for name, value := range map[string]string{
		"control":      "note\nname",
		"too long":     tooLong,
		"invalid utf8": string([]byte{0xff, 0xfe}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := OptionalAlias(&value)
			if err != ErrInvalidAlias {
				t.Fatalf("OptionalAlias(%s) error = %v", name, err)
			}
			if strings.Contains(err.Error(), value) {
				t.Fatalf("error leaked alias: %v", err)
			}
		})
	}
	cleared, err := OptionalAlias(pointer("  "))
	if err != nil || cleared != nil {
		t.Fatalf("OptionalAlias(blank) = %v %v", cleared, err)
	}
	kept, err := OptionalAlias(pointer("  家庭账号  "))
	if err != nil || kept == nil || *kept != "家庭账号" {
		t.Fatalf("OptionalAlias(trim) = %v %v", kept, err)
	}
}

func TestNormalizeManualFieldsStandaloneRequiresEmail(t *testing.T) {
	t.Parallel()
	_, err := NormalizeManualFields(ManualFields{}, ManualStandalone)
	if err != ErrStandaloneEmailRequired {
		t.Fatalf("standalone without email error = %v", err)
	}
	normalized, err := NormalizeManualFields(ManualFields{
		Email: pointer("user@example.com"),
		Plan:  pointer(PlanPlus),
	}, ManualStandalone)
	if err != nil || normalized.Email == nil || normalized.Plan == nil || *normalized.Plan != PlanPlus {
		t.Fatalf("standalone valid = %#v %v", normalized, err)
	}
}

func TestNormalizeManualFieldsLinkedMayClearEmail(t *testing.T) {
	t.Parallel()
	normalized, err := NormalizeManualFields(ManualFields{
		Alias: pointer("work"),
	}, ManualLinkedSupplement)
	if err != nil || normalized.Email != nil || normalized.Alias == nil || *normalized.Alias != "work" {
		t.Fatalf("linked clear email = %#v %v", normalized, err)
	}
}

func TestNormalizeManualFieldsDatePairMustBeTogether(t *testing.T) {
	t.Parallel()
	_, err := NormalizeManualFields(ManualFields{
		Email:          pointer("user@example.com"),
		MembershipDate: pointer("2026-09-15"),
	}, ManualStandalone)
	if err != ErrInvalidDatePair {
		t.Fatalf("date without kind error = %v", err)
	}
	_, err = NormalizeManualFields(ManualFields{
		Email:    pointer("user@example.com"),
		DateKind: pointer(DateKindNextRenewal),
	}, ManualStandalone)
	if err != ErrInvalidDatePair {
		t.Fatalf("kind without date error = %v", err)
	}
	normalized, err := NormalizeManualFields(ManualFields{
		Email:          pointer("user@example.com"),
		MembershipDate: pointer("2026-09-15"),
		DateKind:       pointer(DateKindMembershipExpiry),
	}, ManualStandalone)
	if err != nil || normalized.MembershipDate == nil || *normalized.MembershipDate != "2026-09-15" {
		t.Fatalf("paired date = %#v %v", normalized, err)
	}
}

func TestRequireManualEmailForUnlink(t *testing.T) {
	t.Parallel()
	if err := RequireManualEmailForUnlink(ManualEntry{}); err != ErrUnlinkEmailRequired {
		t.Fatalf("empty unlink error = %v", err)
	}
	if err := RequireManualEmailForUnlink(ManualEntry{Email: pointer("user@example.com")}); err != nil {
		t.Fatalf("present unlink error = %v", err)
	}
}

func TestParseRevisionAndPlanRejectUnknownValues(t *testing.T) {
	t.Parallel()
	if err := ParseRevision(0); err != ErrInvalidRevision {
		t.Fatalf("revision 0 error = %v", err)
	}
	if err := ParsePlan(Plan("ultra")); err != ErrInvalidPlan {
		t.Fatalf("unknown plan error = %v", err)
	}
	if strings.Contains(ErrInvalidPlan.Error(), "ultra") {
		t.Fatal("plan sentinel leaked token")
	}
}

func pointer[T any](value T) *T {
	return &value
}
