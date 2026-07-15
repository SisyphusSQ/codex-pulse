package quota

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const maxWindowMinutes int64 = 525600

type decodedWindow struct {
	usedPercent  float64
	windowMinute int64
	resetsAtMS   int64
}

func decodeWhamUsage(
	content []byte,
	requestID string,
	observedAtMS int64,
) ([]store.QuotaObservationSample, bool) {
	if validateUniqueJSONKeys(content) != nil {
		return nil, true
	}
	envelope, valid := decodeJSONObject(content)
	if !valid {
		return nil, true
	}
	rateLimitRaw, present := envelope["rate_limit"]
	if !present || isNullJSON(rateLimitRaw) {
		return nil, true
	}
	rateLimit, valid := decodeJSONObject(rateLimitRaw)
	if !valid {
		return nil, true
	}
	plan, planTrusted, planValid := decodePlan(envelope["plan_type"])
	if !planValid {
		return nil, true
	}
	primaryRaw := rateLimit["primary_window"]
	secondaryRaw, secondaryKeyPresent := rateLimit["secondary_window"]
	primary, primaryValid := decodeWindow(primaryRaw)
	secondary, secondaryValid := decodeWindow(secondaryRaw)
	secondaryPresent := secondaryKeyPresent && !isNullJSON(secondaryRaw)
	schemaFailure := !primaryValid || secondaryPresent && !secondaryValid
	observations := make([]store.QuotaObservationSample, 0, 2)
	if primaryValid {
		observations = append(observations, newObservation(
			requestID, store.QuotaWindowPrimary, primary, plan, planTrusted, false, observedAtMS,
		))
	}
	if secondaryValid {
		observations = append(observations, newObservation(
			requestID, store.QuotaWindowSecondary, secondary, plan, planTrusted, !primaryValid, observedAtMS,
		))
	}
	if len(observations) == 0 {
		return nil, true
	}
	return observations, schemaFailure
}

func decodePlan(raw json.RawMessage) (string, bool, bool) {
	var plan string
	if !decodeRequiredScalar(raw, &plan) {
		return "unknown", false, false
	}
	plan = strings.ToLower(strings.TrimSpace(plan))
	if plan == "" || len(plan) > 128 {
		return "unknown", false, false
	}
	switch plan {
	case "free", "go", "plus", "pro", "prolite", "team", "self_serve_business_usage_based",
		"business", "enterprise_cbp_usage_based", "enterprise", "edu":
		return plan, true, true
	default:
		return "unknown", false, true
	}
}

func decodeWindow(raw json.RawMessage) (decodedWindow, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || isNullJSON(raw) {
		return decodedWindow{}, false
	}
	wire, valid := decodeJSONObject(raw)
	if !valid {
		return decodedWindow{}, false
	}
	var usedPercent float64
	var windowSeconds int64
	var resetAtSeconds int64
	if !decodeRequiredScalar(wire["used_percent"], &usedPercent) || math.IsNaN(usedPercent) ||
		math.IsInf(usedPercent, 0) || usedPercent < 0 || usedPercent > 100 ||
		!decodeRequiredScalar(wire["limit_window_seconds"], &windowSeconds) || windowSeconds <= 0 ||
		windowSeconds > maxWindowMinutes*60 || !decodeRequiredScalar(wire["reset_at"], &resetAtSeconds) ||
		resetAtSeconds < 0 || resetAtSeconds > math.MaxInt64/1000 {
		return decodedWindow{}, false
	}
	return decodedWindow{
		usedPercent: usedPercent, windowMinute: (windowSeconds + 59) / 60,
		resetsAtMS: resetAtSeconds * 1000,
	}, true
}

func decodeJSONObject(raw []byte) (map[string]json.RawMessage, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || isNullJSON(raw) {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func decodeRequiredScalar(raw json.RawMessage, target any) bool {
	return len(bytes.TrimSpace(raw)) > 0 && !isNullJSON(raw) && json.Unmarshal(raw, target) == nil
}

func newObservation(
	requestID string,
	kind store.QuotaWindowKind,
	window decodedWindow,
	plan string,
	planTrusted bool,
	missingPrimary bool,
	observedAtMS int64,
) store.QuotaObservationSample {
	limitID := "codex"
	requestIDCopy := requestID
	planCopy := plan
	validity := store.QuotaValidityAccepted
	var reason *store.QuotaRejectionReason
	switch {
	case missingPrimary:
		validity = store.QuotaValiditySuspicious
		reason = quotaReason(store.QuotaReasonMissingPrimaryWindow)
	case !planTrusted:
		validity = store.QuotaValiditySuspicious
		reason = quotaReason(store.QuotaReasonUnknownPlanType)
	case window.resetsAtMS <= observedAtMS:
		validity = store.QuotaValiditySuspicious
		reason = quotaReason(store.QuotaReasonResetNotFuture)
	}
	identity := fmt.Sprintf("wham\x00%s\x00%s\x00%d", requestID, kind, observedAtMS)
	return store.QuotaObservationSample{
		ObservationID: "quota-wham-" + store.SHA256DigestOf([]byte(identity)).String(),
		AccountScope:  store.QuotaAccountScopeDefault, Source: store.QuotaSourceWham,
		LimitID: &limitID, WindowKind: kind, UsedPercent: window.usedPercent,
		WindowMinutes: window.windowMinute, ResetsAtMS: window.resetsAtMS, PlanType: &planCopy,
		ObservedAtMS: observedAtMS, Validity: validity, RejectionReason: reason,
		RequestID: &requestIDCopy,
	}
}

func validateUniqueJSONKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := validateJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON object key")
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
		return nil
	default:
		return errors.New("invalid JSON delimiter")
	}
}

func isNullJSON(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func quotaReason(value store.QuotaRejectionReason) *store.QuotaRejectionReason {
	return &value
}
