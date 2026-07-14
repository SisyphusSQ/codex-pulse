package logs

import (
	"fmt"
	"path/filepath"
	"sort"
)

func PlanReconcile(
	confirmedHome string,
	previous []Snapshot,
	discovery DiscoveryResult,
) (ReconcilePlan, error) {
	confirmedHome, err := normalizeConfirmedHome(confirmedHome)
	if err != nil {
		return ReconcilePlan{}, err
	}
	previousByID, previousByPath, err := indexSnapshots(confirmedHome, previous)
	if err != nil {
		return ReconcilePlan{}, err
	}
	_, currentByPath, err := indexSnapshots(confirmedHome, discovery.Snapshots)
	if err != nil {
		return ReconcilePlan{}, err
	}
	issues := append([]DiscoveryIssue(nil), discovery.Issues...)
	for _, issue := range issues {
		if !validIssueForHome(confirmedHome, issue) {
			return ReconcilePlan{}, ErrInvalidSnapshot
		}
	}
	sort.Slice(issues, func(left, right int) bool {
		if issues[left].Path != issues[right].Path {
			return issues[left].Path < issues[right].Path
		}
		if issues[left].Scope != issues[right].Scope {
			return issues[left].Scope == IssueScopeExact
		}
		return issues[left].Code < issues[right].Code
	})

	current := append([]Snapshot(nil), discovery.Snapshots...)
	sort.Slice(current, func(left, right int) bool { return current[left].Path < current[right].Path })
	usedPrevious := make(map[string]struct{}, len(previous))
	usedCurrent := make(map[int]struct{}, len(current))
	usedIssues := make(map[int]struct{}, len(issues))
	plan := ReconcilePlan{Actions: make([]ReconcileAction, 0, len(previous)+len(current)+len(issues))}

	// Match every stable identity before considering path reuse. This prevents
	// one previous source from being consumed by both a move and a replacement
	// when a new file appears at its former path in the same scan.
	for index, snapshot := range current {
		if old, found := previousByID[snapshot.SourceFileID]; found {
			usedPrevious[old.SourceFileID] = struct{}{}
			usedCurrent[index] = struct{}{}
			plan.Actions = append(plan.Actions, reconcileSameIdentity(old, snapshot))
		}
	}
	for index, snapshot := range current {
		if _, used := usedCurrent[index]; used {
			continue
		}
		if old, found := previousByPath[snapshot.Path]; found {
			if _, used := usedPrevious[old.SourceFileID]; !used {
				usedPrevious[old.SourceFileID] = struct{}{}
				plan.Actions = append(plan.Actions, newSnapshotAction(ChangeReplaced, &old, &snapshot, nil))
				continue
			}
		}
		plan.Actions = append(plan.Actions, newSnapshotAction(ChangeAdded, nil, &snapshot, nil))
	}

	orderedPrevious := append([]Snapshot(nil), previous...)
	sort.Slice(orderedPrevious, func(left, right int) bool {
		return orderedPrevious[left].Path < orderedPrevious[right].Path
	})
	for _, old := range orderedPrevious {
		if _, used := usedPrevious[old.SourceFileID]; used {
			continue
		}
		if issueIndex, issue, found := issueCoveringSnapshot(issues, old); found {
			usedIssues[issueIndex] = struct{}{}
			plan.Actions = append(plan.Actions, newSnapshotAction(ChangeUnreadable, &old, nil, &issue))
			continue
		}
		plan.Actions = append(plan.Actions, newSnapshotAction(ChangeDeleted, &old, nil, nil))
	}

	for index, issue := range issues {
		if _, used := usedIssues[index]; used {
			continue
		}
		if _, hasCurrent := currentByPath[issue.Path]; hasCurrent && issue.Scope == IssueScopeExact {
			return ReconcilePlan{}, fmt.Errorf("%w: snapshot and issue share path", ErrInvalidSnapshot)
		}
		issueCopy := issue
		plan.Actions = append(plan.Actions, ReconcileAction{Kind: ChangeUnreadable, Issue: &issueCopy})
	}

	sort.SliceStable(plan.Actions, func(left, right int) bool {
		leftPath := reconcileActionPath(plan.Actions[left])
		rightPath := reconcileActionPath(plan.Actions[right])
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return plan.Actions[left].Kind < plan.Actions[right].Kind
	})
	return plan, nil
}

func indexSnapshots(
	confirmedHome string,
	snapshots []Snapshot,
) (map[string]Snapshot, map[string]Snapshot, error) {
	byID := make(map[string]Snapshot, len(snapshots))
	byPath := make(map[string]Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		if err := validateSnapshotForHome(confirmedHome, snapshot); err != nil {
			return nil, nil, err
		}
		if _, found := byID[snapshot.SourceFileID]; found {
			return nil, nil, fmt.Errorf("%w: duplicate source identity", ErrInvalidSnapshot)
		}
		if _, found := byPath[snapshot.Path]; found {
			return nil, nil, fmt.Errorf("%w: duplicate source path", ErrInvalidSnapshot)
		}
		byID[snapshot.SourceFileID] = snapshot
		byPath[snapshot.Path] = snapshot
	}
	return byID, byPath, nil
}

func reconcileSameIdentity(previous, current Snapshot) ReconcileAction {
	kind := ChangeUnchanged
	prefixRelation := comparePreviousPrefix(previous, current)
	switch {
	case current.Fingerprint.SizeBytes < previous.Fingerprint.SizeBytes:
		kind = ChangeTruncated
	case current.Fingerprint.MTimeNS < previous.Fingerprint.MTimeNS:
		kind = ChangeReplaced
	case current.Fingerprint.SizeBytes > previous.Fingerprint.SizeBytes:
		if prefixRelation == prefixEqual {
			kind = ChangeGrown
		} else {
			kind = ChangeReplaced
		}
	case prefixRelation != prefixEqual:
		kind = ChangeReplaced
	case previous.Path != current.Path || previous.Kind != current.Kind:
		kind = ChangeMoved
	}
	return newSnapshotAction(kind, &previous, &current, nil)
}

type prefixRelation uint8

const (
	prefixUnknown prefixRelation = iota
	prefixEqual
	prefixChanged
)

func comparePreviousPrefix(previous, current Snapshot) prefixRelation {
	if current.Comparison != nil &&
		current.Comparison.PrefixBytes == previous.Fingerprint.PrefixBytes {
		if current.Comparison.PrefixSHA256 == previous.Fingerprint.PrefixSHA256 {
			return prefixEqual
		}
		return prefixChanged
	}
	if previous.Fingerprint.PrefixBytes == current.Fingerprint.PrefixBytes {
		if previous.Fingerprint.PrefixSHA256 == current.Fingerprint.PrefixSHA256 {
			return prefixEqual
		}
		return prefixChanged
	}
	return prefixUnknown
}

func newSnapshotAction(
	kind ChangeKind,
	previous *Snapshot,
	current *Snapshot,
	issue *DiscoveryIssue,
) ReconcileAction {
	action := ReconcileAction{Kind: kind}
	if previous != nil {
		action.Previous = cloneSnapshot(*previous)
	}
	if current != nil {
		action.Current = cloneSnapshot(*current)
	}
	if issue != nil {
		copy := *issue
		action.Issue = &copy
	}
	action.PathChanged = action.Previous != nil && action.Current != nil &&
		action.Previous.Path != action.Current.Path
	return action
}

func cloneSnapshot(snapshot Snapshot) *Snapshot {
	copy := snapshot
	if snapshot.Comparison != nil {
		comparison := *snapshot.Comparison
		copy.Comparison = &comparison
	}
	return &copy
}

func issueCoveringSnapshot(
	issues []DiscoveryIssue,
	snapshot Snapshot,
) (int, DiscoveryIssue, bool) {
	for index, issue := range issues {
		if issue.SourceFileID != "" && issue.SourceFileID == snapshot.SourceFileID {
			return index, issue, true
		}
		if issue.Scope == IssueScopeExact && issue.Path == snapshot.Path {
			return index, issue, true
		}
		if issue.Scope == IssueScopeSubtree && pathWithin(issue.Path, snapshot.Path) {
			return index, issue, true
		}
	}
	return 0, DiscoveryIssue{}, false
}

func reconcileActionPath(action ReconcileAction) string {
	if action.Current != nil {
		return action.Current.Path
	}
	if action.Previous != nil {
		return action.Previous.Path
	}
	if action.Issue != nil {
		return action.Issue.Path
	}
	return filepath.Clean(string(filepath.Separator))
}
