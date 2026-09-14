package quota

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func observationsFromRateLimits(
	snapshot appserver.AccountRateLimitsSnapshot,
	request BoundRefreshRequest,
	observedAtMS int64,
) ([]store.QuotaObservationSample, bool) {
	buckets := rateLimitBuckets(snapshot)
	if len(buckets) == 0 {
		return nil, true
	}
	var observations []store.QuotaObservationSample
	schemaFailure := false
	for _, bucket := range buckets {
		if bucket.LimitID == nil || *bucket.LimitID == "" {
			schemaFailure = true
			continue
		}
		limitID := *bucket.LimitID
		plan, planTrusted, planValid := decodeAppServerPlan(bucket.PlanType)
		if !planValid {
			schemaFailure = true
			continue
		}
		primary, primaryOK := decodeAppServerWindow(bucket.Primary)
		secondary, secondaryOK := decodeAppServerWindow(bucket.Secondary)
		if !primaryOK && bucket.Primary != nil {
			schemaFailure = true
		}
		if !secondaryOK && bucket.Secondary != nil {
			schemaFailure = true
		}
		if primaryOK {
			observations = append(observations, newAppServerObservation(
				request, limitID, bucket.LimitName, store.QuotaWindowPrimary,
				primary, plan, planTrusted, false, observedAtMS,
			))
		}
		if secondaryOK {
			observations = append(observations, newAppServerObservation(
				request, limitID, bucket.LimitName, store.QuotaWindowSecondary,
				secondary, plan, planTrusted, !primaryOK, observedAtMS,
			))
		}
		if !primaryOK {
			schemaFailure = true
		}
	}
	return observations, schemaFailure
}

func rateLimitBuckets(snapshot appserver.AccountRateLimitsSnapshot) []appserver.RateLimitSnapshot {
	if snapshot.RateLimitsByLimitID != nil {
		limitIDs := make([]string, 0, len(snapshot.RateLimitsByLimitID))
		for limitID := range snapshot.RateLimitsByLimitID {
			limitIDs = append(limitIDs, limitID)
		}
		sort.Strings(limitIDs)
		buckets := make([]appserver.RateLimitSnapshot, 0, len(limitIDs))
		for _, limitID := range limitIDs {
			bucket := snapshot.RateLimitsByLimitID[limitID]
			if bucket.LimitID == nil {
				copied := limitID
				bucket.LimitID = &copied
			}
			buckets = append(buckets, bucket)
		}
		return buckets
	}
	bucket := snapshot.RateLimits
	if bucket.LimitID == nil {
		limitID := "codex"
		bucket.LimitID = &limitID
	}
	return []appserver.RateLimitSnapshot{bucket}
}

func decodeAppServerWindow(window *appserver.RateLimitWindow) (decodedWindow, bool) {
	if window == nil {
		return decodedWindow{}, false
	}
	if window.WindowDurationMins == nil || *window.WindowDurationMins <= 0 ||
		*window.WindowDurationMins > maxWindowMinutes || window.ResetsAtSeconds == nil ||
		*window.ResetsAtSeconds < 0 {
		return decodedWindow{}, false
	}
	return decodedWindow{
		usedPercent:  float64(window.UsedPercent),
		windowMinute: *window.WindowDurationMins,
		resetsAtMS:   *window.ResetsAtSeconds * 1000,
	}, true
}

func decodeAppServerPlan(value *string) (string, bool, bool) {
	if value == nil {
		return "unknown", false, true
	}
	plan := strings.ToLower(strings.TrimSpace(*value))
	if plan == "" {
		return "unknown", false, true
	}
	switch plan {
	case "free", "go", "plus", "pro", "prolite", "team", "self_serve_business_usage_based",
		"business", "enterprise_cbp_usage_based", "enterprise", "edu":
		return plan, true, true
	default:
		return "unknown", false, true
	}
}

func newAppServerObservation(
	request BoundRefreshRequest,
	limitID string,
	limitName *string,
	kind store.QuotaWindowKind,
	window decodedWindow,
	plan string,
	planTrusted bool,
	missingPrimary bool,
	observedAtMS int64,
) store.QuotaObservationSample {
	requestIDCopy := request.RequestID
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
	identity := fmt.Sprintf(
		"app_server\x00%s\x00%d\x00%s\x00%s\x00%s\x00%d",
		request.Binding.AccountScope, request.Binding.BindingGeneration, request.RequestID,
		limitID, kind, observedAtMS,
	)
	return store.QuotaObservationSample{
		ObservationID: "quota-app-server-" + store.SHA256DigestOf([]byte(identity)).String(),
		AccountScope:  request.Binding.AccountScope, Source: store.QuotaSourceAppServer,
		LimitID: &limitID, LimitName: cloneOptionalString(limitName),
		WindowKind: kind, UsedPercent: window.usedPercent,
		WindowMinutes: window.windowMinute, ResetsAtMS: window.resetsAtMS, PlanType: &planCopy,
		ObservedAtMS: observedAtMS, Validity: validity, RejectionReason: reason,
		RequestID: &requestIDCopy,
	}
}

func quotaTypedDigest(
	request BoundRefreshRequest,
	observedAtMS int64,
	observations []store.QuotaObservationSample,
) store.SHA256Digest {
	var builder strings.Builder
	fmt.Fprintf(
		&builder, "app_server\x00%s\x00%d\x00%s\x00%d",
		request.Binding.AccountScope, request.Binding.BindingGeneration, request.RequestID, observedAtMS,
	)
	for _, observation := range observations {
		limitID := ""
		if observation.LimitID != nil {
			limitID = *observation.LimitID
		}
		fmt.Fprintf(
			&builder, "\x00%s\x00%s\x00%g\x00%d\x00%d",
			limitID, observation.WindowKind, observation.UsedPercent, observation.WindowMinutes, observation.ResetsAtMS,
		)
	}
	return store.SHA256DigestOf([]byte(builder.String()))
}

type decodedWindow struct {
	usedPercent  float64
	windowMinute int64
	resetsAtMS   int64
}

const maxWindowMinutes int64 = 525600

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func quotaReason(value store.QuotaRejectionReason) *store.QuotaRejectionReason {
	return &value
}
