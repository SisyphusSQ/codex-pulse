// Package attribution converts local parser metadata into versioned, explainable
// derived identities. It performs no filesystem or database I/O.
package attribution

const RuleVersion = 1

type Confidence string

const (
	ConfidenceHigh    Confidence = "high"
	ConfidenceMedium  Confidence = "medium"
	ConfidenceLow     Confidence = "low"
	ConfidenceUnknown Confidence = "unknown"
)

type Source string

const (
	SourceSessionIDFallback Source = "session_id_fallback"
	SourceRegisteredRoot    Source = "registered_root"
	SourceCWDPathDigest     Source = "cwd_path_digest"
	SourceModelCanonical    Source = "model_canonical"
	SourceModelAlias        Source = "model_alias"
	SourceConflict          Source = "conflict"
	SourceMissing           Source = "missing"
	SourceInvalidPath       Source = "invalid_path"
	SourceInvalidModel      Source = "invalid_model"
)

type Reason string

const (
	ReasonStableIdentity Reason = "stable_identity"
	ReasonRootMatched    Reason = "root_matched"
	ReasonPathDerived    Reason = "path_derived"
	ReasonObserved       Reason = "observed"
	ReasonConflict       Reason = "conflict"
	ReasonMissing        Reason = "missing"
	ReasonInvalid        Reason = "invalid"
)

type Candidate struct {
	Key         string
	DisplayName string
	Priority    int
	Confidence  Confidence
	Source      Source
	Reason      Reason
}

type Decision struct {
	Key         string
	DisplayName string
	Confidence  Confidence
	Source      Source
	Reason      Reason
}

func unknownDecision() Decision {
	return Decision{
		Confidence: ConfidenceUnknown,
		Source:     SourceMissing,
		Reason:     ReasonMissing,
	}
}

func conflictDecision() Decision {
	return Decision{
		Confidence: ConfidenceLow,
		Source:     SourceConflict,
		Reason:     ReasonConflict,
	}
}
