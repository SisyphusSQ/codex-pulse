package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Analyze 比较 Store expected names 和 index latest view，只生成可证明安全的 append actions。
func Analyze(
	parsed ParsedIndex,
	expected []Expectation,
	version FileVersion,
	analyzedAtMS int64,
) (RepairPlan, error) {
	if analyzedAtMS < 0 || !validFileVersion(version) || !validDiagnostics(parsed.Diagnostics) {
		return RepairPlan{}, ErrInvalidPlan
	}
	expectations := make([]Expectation, len(expected))
	copy(expectations, expected)
	sort.Slice(expectations, func(left, right int) bool {
		return expectations[left].SessionID < expectations[right].SessionID
	})
	for index, expectation := range expectations {
		if !validExpectation(expectation) {
			return RepairPlan{}, ErrInvalidExpectation
		}
		if index > 0 && expectations[index-1].SessionID == expectation.SessionID {
			return RepairPlan{}, ErrInvalidExpectation
		}
	}

	plan := RepairPlan{
		AnalyzedAtMS:       analyzedAtMS,
		ExpectationsSHA256: expectationDigest(expectations),
		Source:             version,
		Diagnostics:        append([]Diagnostic(nil), parsed.Diagnostics...),
	}
	for _, expectation := range expectations {
		latest, found := parsed.Latest(expectation.SessionID)
		switch {
		case !found:
			plan.Actions = append(plan.Actions, actionFromExpectation(expectation, ReasonMissing))
		case latest.ThreadName == expectation.ThreadName:
			// latest name 已对齐；较旧历史行不需要 destructive compaction。
		case latest.UpdatedAtMS != nil && expectation.UpdatedAtMS > *latest.UpdatedAtMS:
			plan.Actions = append(plan.Actions, actionFromExpectation(expectation, ReasonStale))
		default:
			plan.Conflicts = append(plan.Conflicts, Conflict{
				SessionID: expectation.SessionID,
				Reason:    ConflictIndexNewerOrUnknown,
			})
		}
	}
	for sessionID, count := range parsed.history {
		if count > 1 {
			plan.Histories = append(plan.Histories, History{SessionID: sessionID, Count: count})
		}
	}
	sort.Slice(plan.Histories, func(left, right int) bool {
		return plan.Histories[left].SessionID < plan.Histories[right].SessionID
	})
	if err := validateRepairCapacity(plan.Source, plan.Actions); err != nil {
		return RepairPlan{}, err
	}
	plan.ID = planDigest(plan)
	if err := VerifyPlan(plan); err != nil {
		return RepairPlan{}, err
	}
	return plan, nil
}

// VerifyPlan 重新计算 digest，拒绝在 dry-run 后被修改的 DTO。
func VerifyPlan(plan RepairPlan) error {
	if !validPlanStructure(plan) || plan.ID != planDigest(plan) {
		return ErrInvalidPlan
	}
	return nil
}

func validPlanStructure(plan RepairPlan) bool {
	if plan.ID == "" || !validSHA256(plan.ID) || !validSHA256(plan.ExpectationsSHA256) ||
		plan.AnalyzedAtMS < 0 ||
		!validFileVersion(plan.Source) || !validDiagnostics(plan.Diagnostics) {
		return false
	}
	seen := make(map[string]struct{}, len(plan.Actions)+len(plan.Conflicts))
	previous := ""
	for _, action := range plan.Actions {
		if action.SessionID <= previous || !validRepairAction(action) {
			return false
		}
		seen[action.SessionID] = struct{}{}
		previous = action.SessionID
	}
	previous = ""
	for _, conflict := range plan.Conflicts {
		if conflict.SessionID <= previous || conflict.Reason != ConflictIndexNewerOrUnknown {
			return false
		}
		if _, exists := seen[conflict.SessionID]; exists {
			return false
		}
		if _, err := uuid.Parse(conflict.SessionID); err != nil {
			return false
		}
		seen[conflict.SessionID] = struct{}{}
		previous = conflict.SessionID
	}
	previous = ""
	for _, history := range plan.Histories {
		if history.SessionID <= previous || history.Count <= 1 {
			return false
		}
		if _, err := uuid.Parse(history.SessionID); err != nil {
			return false
		}
		previous = history.SessionID
	}
	return validateRepairCapacity(plan.Source, plan.Actions) == nil
}

func validateRepairCapacity(source FileVersion, actions []RepairAction) error {
	if len(actions) == 0 {
		return nil
	}
	entries := make([]Entry, 0, len(actions))
	for _, action := range actions {
		entries = append(entries, Entry{
			ID: action.SessionID, ThreadName: action.ThreadName, UpdatedAt: action.UpdatedAt,
		})
	}
	payload, err := canonicalEntries(entries)
	if err != nil {
		return err
	}
	payloadSize := len(payload)
	if source.Exists && source.SizeBytes > 0 {
		// Analyze 没有持有原始尾字节；保守预留一个缺失换行的 separator。
		payloadSize++
	}
	return validateAppendSize(source.SizeBytes, payloadSize)
}

func expectationDigest(expectations []Expectation) string {
	encoded, err := json.Marshal(expectations)
	if err != nil {
		panic(errors.New("session index expectations contain an unsupported value"))
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func validRepairAction(action RepairAction) bool {
	if action.Reason != ReasonMissing && action.Reason != ReasonStale {
		return false
	}
	if _, err := uuid.Parse(action.SessionID); err != nil {
		return false
	}
	if strings.TrimSpace(action.ThreadName) == "" || len(action.ThreadName) > maxThreadNameBytes {
		return false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, action.UpdatedAt)
	return err == nil && updatedAt.UnixMilli() >= 0 &&
		updatedAt.UTC().Format(time.RFC3339Nano) == action.UpdatedAt
}

func validDiagnostics(diagnostics []Diagnostic) bool {
	previousLine := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Line <= previousLine || diagnostic.Raw != "" || !validDiagnosticCode(diagnostic.Code) {
			return false
		}
		previousLine = diagnostic.Line
	}
	return true
}

func validDiagnosticCode(code DiagnosticCode) bool {
	switch code {
	case DiagnosticMalformedJSON, DiagnosticInvalidID,
		DiagnosticInvalidThreadName, DiagnosticInvalidUpdatedAt:
		return true
	default:
		return false
	}
}

func actionFromExpectation(expectation Expectation, reason RepairReason) RepairAction {
	return RepairAction{
		SessionID: expectation.SessionID, ThreadName: expectation.ThreadName,
		UpdatedAt: time.UnixMilli(expectation.UpdatedAtMS).UTC().Format(time.RFC3339Nano), Reason: reason,
	}
}

func validExpectation(expectation Expectation) bool {
	_, err := uuid.Parse(expectation.SessionID)
	return err == nil && strings.TrimSpace(expectation.ThreadName) != "" &&
		len(expectation.ThreadName) <= maxThreadNameBytes && expectation.UpdatedAtMS >= 0
}

func validFileVersion(version FileVersion) bool {
	if !version.Exists {
		return version.DeviceID == "" && version.Inode == 0 && version.SizeBytes == 0 &&
			version.MTimeNS == 0 && version.SHA256 == ""
	}
	return version.DeviceID != "" && version.Inode >= 0 && version.SizeBytes >= 0 &&
		version.MTimeNS >= 0 && validSHA256(version.SHA256)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

func planDigest(plan RepairPlan) string {
	copy := plan
	copy.ID = ""
	encoded, err := json.Marshal(copy)
	if err != nil {
		panic(errors.New("session index plan contains an unsupported value"))
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
