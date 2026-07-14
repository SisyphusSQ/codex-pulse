package attribution

import "sort"

// Arbitrate keeps only the highest-priority candidates and refuses to choose
// when peers identify different values. Lower-priority fallbacks never mask a
// conflict among authoritative peers.
func Arbitrate(candidates []Candidate) Decision {
	maximumPriority := 0
	hasCandidate := false
	for _, candidate := range candidates {
		if !meaningfulCandidate(candidate) {
			continue
		}
		if !hasCandidate || candidate.Priority > maximumPriority {
			maximumPriority = candidate.Priority
			hasCandidate = true
		}
	}
	if !hasCandidate {
		return unknownDecision()
	}

	peers := make([]Candidate, 0, len(candidates))
	identities := make(map[string]struct{})
	hasUnresolvedPeer := false
	for _, candidate := range candidates {
		if !meaningfulCandidate(candidate) || candidate.Priority != maximumPriority {
			continue
		}
		peers = append(peers, candidate)
		if candidate.Key == "" {
			hasUnresolvedPeer = true
		} else {
			identities[candidate.Key] = struct{}{}
		}
	}
	if len(identities) > 1 || len(identities) == 1 && hasUnresolvedPeer {
		return conflictDecision()
	}

	sortCandidates(peers)
	chosen := peers[0]
	reason := chosen.Reason
	if reason == "" {
		reason = ReasonObserved
	}
	return Decision{
		Key: chosen.Key, DisplayName: chosen.DisplayName,
		Confidence: chosen.Confidence, Source: chosen.Source, Reason: reason,
	}
}

func meaningfulCandidate(candidate Candidate) bool {
	return candidate.Key != "" || candidate.Source != "" && candidate.Source != SourceMissing
}

func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(left, right int) bool {
		leftRank := confidenceRank(candidates[left].Confidence)
		rightRank := confidenceRank(candidates[right].Confidence)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if candidates[left].Source != candidates[right].Source {
			return candidates[left].Source < candidates[right].Source
		}
		return candidates[left].DisplayName < candidates[right].DisplayName
	})
}

func confidenceRank(value Confidence) int {
	switch value {
	case ConfidenceHigh:
		return 4
	case ConfidenceMedium:
		return 3
	case ConfidenceLow:
		return 2
	case ConfidenceUnknown:
		return 1
	default:
		return 0
	}
}
