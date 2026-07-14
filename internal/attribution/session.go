package attribution

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type SessionTitleDecision struct {
	DisplayTitle string
	Confidence   Confidence
	Source       Source
	Reason       Reason
}

func NormalizeSessionTitle(sessionID string) SessionTitleDecision {
	if sessionID == "" {
		return SessionTitleDecision{
			Confidence: ConfidenceUnknown, Source: SourceMissing, Reason: ReasonMissing,
		}
	}
	digest := sha256.Sum256([]byte("session-title-v1\x00" + sessionID))
	return SessionTitleDecision{
		DisplayTitle: "Session " + strings.ToUpper(hex.EncodeToString(digest[:4])),
		Confidence:   ConfidenceHigh, Source: SourceSessionIDFallback, Reason: ReasonStableIdentity,
	}
}
