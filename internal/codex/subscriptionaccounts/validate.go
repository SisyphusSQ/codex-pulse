package subscriptionaccounts

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalidUUID              = errors.New("invalid Codex subscription UUID")
	ErrInvalidEmail             = errors.New("invalid Codex subscription email")
	ErrInvalidAlias             = errors.New("invalid Codex subscription alias")
	ErrInvalidPlan              = errors.New("invalid Codex subscription plan")
	ErrInvalidMembershipDate    = errors.New("invalid Codex subscription membership date")
	ErrInvalidDateKind          = errors.New("invalid Codex subscription date kind")
	ErrInvalidDatePair          = errors.New("invalid Codex subscription date pair")
	ErrInvalidRevision          = errors.New("invalid Codex subscription revision")
	ErrInvalidTimeZone          = errors.New("invalid Codex subscription time zone")
	ErrInvalidEvaluationTime    = errors.New("invalid Codex subscription evaluation time")
	ErrStandaloneEmailRequired  = errors.New("Codex subscription standalone email is required")
	ErrUnlinkEmailRequired      = errors.New("manual_email_required_before_unlink")
	ErrAccountBindingChanged    = errors.New("Codex account binding changed")
	ErrInvalidAutomaticPlanFact = errors.New("invalid Codex subscription automatic plan fact")
)

const (
	maxEmailBytes = 320
	maxAliasBytes = 128
)

func ParsePublicID(value string) (string, error) {
	if len(value) != 36 {
		return "", ErrInvalidUUID
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return "", ErrInvalidUUID
	}
	return parsed.String(), nil
}

func NormalizeEmail(value string) (display string, matchKey string, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", ErrInvalidEmail
	}
	if err := validateEmailShape(trimmed); err != nil {
		return "", "", err
	}
	return trimmed, strings.ToLower(trimmed), nil
}

func OptionalEmail(value *string) (display *string, matchKey *string, err error) {
	if value == nil {
		return nil, nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil, nil
	}
	normalized, key, err := NormalizeEmail(trimmed)
	if err != nil {
		return nil, nil, err
	}
	return pointerTo(normalized), pointerTo(key), nil
}

func OptionalAlias(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if !utf8.ValidString(trimmed) || len(trimmed) > maxAliasBytes {
		return nil, ErrInvalidAlias
	}
	for _, character := range trimmed {
		if unicode.IsControl(character) {
			return nil, ErrInvalidAlias
		}
	}
	return pointerTo(trimmed), nil
}

func ParsePlan(value Plan) error {
	if !validPlan(value) {
		return ErrInvalidPlan
	}
	return nil
}

func ParseDateKind(value DateKind) error {
	switch value {
	case DateKindNextRenewal, DateKindMembershipExpiry:
		return nil
	default:
		return ErrInvalidDateKind
	}
}

func ParseRevision(value int64) error {
	if value <= 0 {
		return ErrInvalidRevision
	}
	return nil
}

func NormalizeManualFields(fields ManualFields, role ManualRole) (NormalizedManual, error) {
	email, matchKey, err := OptionalEmail(fields.Email)
	if err != nil {
		return NormalizedManual{}, err
	}
	if role == ManualStandalone && email == nil {
		return NormalizedManual{}, ErrStandaloneEmailRequired
	}
	alias, err := OptionalAlias(fields.Alias)
	if err != nil {
		return NormalizedManual{}, err
	}
	var plan *Plan
	if fields.Plan != nil {
		if err := ParsePlan(*fields.Plan); err != nil {
			return NormalizedManual{}, err
		}
		copied := *fields.Plan
		plan = &copied
	}
	date, kind, err := normalizeDatePair(fields.MembershipDate, fields.DateKind)
	if err != nil {
		return NormalizedManual{}, err
	}
	return NormalizedManual{
		Email:          email,
		EmailMatchKey:  matchKey,
		Alias:          alias,
		Plan:           plan,
		MembershipDate: date,
		DateKind:       kind,
	}, nil
}

func RequireManualEmailForUnlink(entry ManualEntry) error {
	if entry.Email == nil || strings.TrimSpace(*entry.Email) == "" {
		return ErrUnlinkEmailRequired
	}
	return nil
}

func normalizeDatePair(date *string, kind *DateKind) (*string, *DateKind, error) {
	trimmedDate := optionalTrimmed(date)
	hasDate := trimmedDate != nil
	hasKind := kind != nil
	if hasDate != hasKind {
		return nil, nil, ErrInvalidDatePair
	}
	if !hasDate {
		return nil, nil, nil
	}
	parsed, err := ParseCivilDate(*trimmedDate)
	if err != nil {
		return nil, nil, err
	}
	if err := ParseDateKind(*kind); err != nil {
		return nil, nil, err
	}
	encoded := parsed.String()
	copiedKind := *kind
	return &encoded, &copiedKind, nil
}

func validateEmailShape(value string) error {
	if !utf8.ValidString(value) || len(value) < 1 || len(value) > maxEmailBytes {
		return ErrInvalidEmail
	}
	at := 0
	for index, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return ErrInvalidEmail
		}
		if character == '@' {
			at++
			if index == 0 || index == len(value)-1 {
				return ErrInvalidEmail
			}
		}
	}
	if at != 1 {
		return ErrInvalidEmail
	}
	return nil
}

func optionalTrimmed(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validPlan(value Plan) bool {
	switch value {
	case PlanFree, PlanGo, PlanPlus, PlanPro5X, PlanPro20X, PlanTeam, PlanBusiness, PlanEnterprise, PlanEdu:
		return true
	default:
		return false
	}
}

func pointerTo[T any](value T) *T {
	return &value
}
