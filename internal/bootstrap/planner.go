// Package bootstrap coordinates the durable first-index plan without owning the later live scheduler.
package bootstrap

import (
	"errors"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidPlan = errors.New("invalid bootstrap plan")

type PlanRequest struct {
	JobID        string
	Reconcile    logs.ReconcilePlan
	NowMS        int64
	DayStartMS   int64
	FastMaxFiles int
	FastMaxBytes int64
	RecencyHints map[string]int64
	// CommittedOffsets only contains active sources whose authoritative cursor
	// has not reached the current snapshot size.
	CommittedOffsets map[string]int64
	AtMS             int64
}

type ReconcilePlanRequest struct {
	JobID        string
	Reconcile    logs.ReconcilePlan
	StartOrdinal int64
	Pass         int64
	AtMS         int64
}

type planCandidate struct {
	item      store.BootstrapPlanItem
	recencyMS int64
	fastRank  int
	tierRank  int
	sourceID  string
}

// FreezeInitialPlan converts one pure reconcile snapshot into a deterministic fixed plan.
// session_index.jsonl remains a hint source and is never emitted as an ingest item.
func FreezeInitialPlan(request PlanRequest) ([]store.BootstrapPlanItem, error) {
	if request.JobID == "" || request.NowMS < 0 || request.DayStartMS < 0 ||
		request.DayStartMS > request.NowMS || request.FastMaxFiles <= 0 ||
		request.FastMaxBytes <= 0 || request.AtMS < 0 {
		return nil, ErrInvalidPlan
	}
	candidates := make([]planCandidate, 0, len(request.Reconcile.Actions))
	for _, action := range request.Reconcile.Actions {
		if actionUsesSessionIndex(action) {
			continue
		}
		sourceID := snapshotSourceIDFromAction(action)
		committedOffset, incomplete := request.CommittedOffsets[sourceID]
		if action.Kind == logs.ChangeUnchanged && !incomplete {
			continue
		}
		kind, ok := bootstrapActionKind(action.Kind)
		if !ok {
			return nil, ErrInvalidPlan
		}
		previous, err := bootstrapFingerprint(action.Previous)
		if err != nil {
			return nil, err
		}
		current, err := bootstrapFingerprint(action.Current)
		if err != nil {
			return nil, err
		}
		if !validPlanSnapshots(kind, previous, current) {
			return nil, ErrInvalidPlan
		}
		recencyMS := snapshotRecencyMS(previous, current)
		sourceID = snapshotSourceID(previous, current)
		if incomplete && (committedOffset < 0 || current == nil || committedOffset >= current.SizeBytes) {
			return nil, ErrInvalidPlan
		}
		if hinted, found := request.RecencyHints[sourceID]; found {
			if hinted < 0 || hinted > request.NowMS {
				return nil, ErrInvalidPlan
			}
			if hinted > recencyMS {
				recencyMS = hinted
			}
		}
		tier, tierRank := bootstrapTier(recencyMS, request.DayStartMS, request.NowMS)
		progressTotal := int64(0)
		if current != nil {
			progressTotal = current.SizeBytes
		}
		fastRank := 1
		if action.Kind == logs.ChangeGrown || incomplete {
			fastRank = 0
			tier = store.BootstrapTierActiveAppend
			tierRank = -1
		}
		candidates = append(candidates, planCandidate{
			item: store.BootstrapPlanItem{
				JobID: request.JobID, Pass: 0, Tier: tier, ActionKind: kind,
				Previous: previous, Current: current, State: store.BootstrapItemQueued,
				ProgressTotal: progressTotal, UpdatedAtMS: request.AtMS,
			},
			recencyMS: recencyMS, fastRank: fastRank, tierRank: tierRank, sourceID: sourceID,
		})
	}

	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].fastRank != candidates[right].fastRank {
			return candidates[left].fastRank < candidates[right].fastRank
		}
		if candidates[left].recencyMS != candidates[right].recencyMS {
			return candidates[left].recencyMS > candidates[right].recencyMS
		}
		return candidates[left].sourceID < candidates[right].sourceID
	})

	fast := make([]planCandidate, 0, min(request.FastMaxFiles, len(candidates)))
	backfill := make([]planCandidate, 0, len(candidates))
	var fastBytes int64
	for _, candidate := range candidates {
		fitsFiles := len(fast) < request.FastMaxFiles
		fitsBytes := candidate.item.ProgressTotal <= request.FastMaxBytes-fastBytes
		if fitsFiles && (fitsBytes || len(fast) == 0) {
			candidate.item.Lane = store.BootstrapLaneFast
			fast = append(fast, candidate)
			fastBytes += candidate.item.ProgressTotal
			continue
		}
		candidate.item.Lane = store.BootstrapLaneBackfill
		backfill = append(backfill, candidate)
	}
	sort.Slice(backfill, func(left, right int) bool {
		if backfill[left].tierRank != backfill[right].tierRank {
			return backfill[left].tierRank < backfill[right].tierRank
		}
		if backfill[left].recencyMS != backfill[right].recencyMS {
			return backfill[left].recencyMS > backfill[right].recencyMS
		}
		return backfill[left].sourceID < backfill[right].sourceID
	})
	items := make([]store.BootstrapPlanItem, 0, len(candidates))
	for _, candidate := range append(fast, backfill...) {
		candidate.item.Ordinal = int64(len(items))
		items = append(items, candidate.item)
	}
	return items, nil
}

func snapshotSourceIDFromAction(action logs.ReconcileAction) string {
	if action.Current != nil {
		return action.Current.SourceFileID
	}
	if action.Previous != nil {
		return action.Previous.SourceFileID
	}
	return ""
}

// FreezeReconcilePlan converts the final directory snapshot into a fixed pass.
// PlanReconcile already provides deterministic action order, which is preserved.
func FreezeReconcilePlan(request ReconcilePlanRequest) ([]store.BootstrapPlanItem, error) {
	if request.JobID == "" || request.StartOrdinal < 0 || request.Pass < 1 || request.AtMS < 0 {
		return nil, ErrInvalidPlan
	}
	items := make([]store.BootstrapPlanItem, 0, len(request.Reconcile.Actions))
	for _, action := range request.Reconcile.Actions {
		if action.Kind == logs.ChangeUnchanged || actionUsesSessionIndex(action) {
			continue
		}
		// A discovery issue with no previously known source has no executable
		// source action. The caller persists it in ReconcileIssueCount instead.
		if action.Kind == logs.ChangeUnreadable && action.Previous == nil && action.Current == nil {
			continue
		}
		kind, ok := bootstrapActionKind(action.Kind)
		if !ok {
			return nil, ErrInvalidPlan
		}
		previous, err := bootstrapFingerprint(action.Previous)
		if err != nil {
			return nil, err
		}
		current, err := bootstrapFingerprint(action.Current)
		if err != nil {
			return nil, err
		}
		if !validPlanSnapshots(kind, previous, current) {
			return nil, ErrInvalidPlan
		}
		progressTotal := int64(0)
		if current != nil {
			progressTotal = current.SizeBytes
		}
		items = append(items, store.BootstrapPlanItem{
			JobID: request.JobID, Ordinal: request.StartOrdinal + int64(len(items)), Pass: request.Pass,
			Lane: store.BootstrapLaneReconcile, Tier: store.BootstrapTierReconcile,
			ActionKind: kind, Previous: previous, Current: current,
			State: store.BootstrapItemQueued, ProgressTotal: progressTotal, UpdatedAtMS: request.AtMS,
		})
	}
	return items, nil
}

func actionUsesSessionIndex(action logs.ReconcileAction) bool {
	return action.Previous != nil && action.Previous.Kind == logs.SourceKindSessionIndex ||
		action.Current != nil && action.Current.Kind == logs.SourceKindSessionIndex
}

func bootstrapActionKind(kind logs.ChangeKind) (store.BootstrapActionKind, bool) {
	switch kind {
	case logs.ChangeAdded:
		return store.BootstrapActionAdded, true
	case logs.ChangeUnchanged:
		return store.BootstrapActionUnchanged, true
	case logs.ChangeGrown:
		return store.BootstrapActionGrown, true
	case logs.ChangeTruncated:
		return store.BootstrapActionTruncated, true
	case logs.ChangeMoved:
		return store.BootstrapActionMoved, true
	case logs.ChangeReplaced:
		return store.BootstrapActionReplaced, true
	case logs.ChangeDeleted:
		return store.BootstrapActionDeleted, true
	case logs.ChangeUnreadable:
		return store.BootstrapActionUnreadable, true
	default:
		return "", false
	}
}

func bootstrapFingerprint(snapshot *logs.Snapshot) (*store.SourceFingerprint, error) {
	if snapshot == nil {
		return nil, nil
	}
	if snapshot.Kind != logs.SourceKindSession && snapshot.Kind != logs.SourceKindArchivedSession {
		return nil, ErrInvalidPlan
	}
	value := store.SourceFingerprint{
		SourceFileID: snapshot.SourceFileID, Provider: snapshot.Provider, SourceKind: string(snapshot.Kind),
		CurrentPath: snapshot.Path, DeviceID: snapshot.Fingerprint.DeviceID,
		Inode: snapshot.Fingerprint.Inode, SizeBytes: snapshot.Fingerprint.SizeBytes,
		MTimeNS: snapshot.Fingerprint.MTimeNS, PrefixBytes: snapshot.Fingerprint.PrefixBytes,
		PrefixSHA256:      snapshot.Fingerprint.PrefixSHA256,
		FingerprintSHA256: snapshot.Fingerprint.Digest,
	}
	return &value, nil
}

func validPlanSnapshots(
	kind store.BootstrapActionKind,
	previous *store.SourceFingerprint,
	current *store.SourceFingerprint,
) bool {
	switch kind {
	case store.BootstrapActionAdded:
		return previous == nil && current != nil
	case store.BootstrapActionDeleted, store.BootstrapActionUnreadable:
		return previous != nil && current == nil
	default:
		return previous != nil && current != nil
	}
}

func snapshotRecencyMS(previous, current *store.SourceFingerprint) int64 {
	value := previous
	if current != nil {
		value = current
	}
	if value == nil {
		return 0
	}
	return value.MTimeNS / int64(timeNanosecondPerMillisecond)
}

const timeNanosecondPerMillisecond = 1_000_000

func snapshotSourceID(previous, current *store.SourceFingerprint) string {
	if current != nil {
		return current.SourceFileID
	}
	if previous != nil {
		return previous.SourceFileID
	}
	return ""
}

func bootstrapTier(recencyMS, dayStartMS, nowMS int64) (store.BootstrapTier, int) {
	const dayMS = int64(24 * 60 * 60 * 1000)
	switch {
	case recencyMS >= dayStartMS:
		return store.BootstrapTierToday, 0
	case recencyMS >= nowMS-7*dayMS:
		return store.BootstrapTierRecent7Days, 1
	case recencyMS >= nowMS-30*dayMS:
		return store.BootstrapTierRecent30Days, 2
	default:
		return store.BootstrapTierOlder, 3
	}
}
