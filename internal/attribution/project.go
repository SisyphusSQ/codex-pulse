package attribution

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	projectIdentityNamespace = "local-path-v1\x00"
	maxProjectDisplayRunes   = 80
)

type ProjectRoot struct {
	ProjectID   string
	RootPath    string
	DisplayName string
	Confidence  Confidence
}

type ProjectInput struct {
	CWD   string
	Roots []ProjectRoot
}

// ProjectDecision contains RootPath because it is consumed by the local Store
// adapter. Callers must expose only ProjectID and DisplayName outside that
// controlled matching boundary.
type ProjectDecision struct {
	ProjectID   string
	DisplayName string
	RootPath    string
	Confidence  Confidence
	Source      Source
	Reason      Reason
}

func ResolveProject(input ProjectInput) ProjectDecision {
	cwd, valid := normalizeAbsolutePath(input.CWD)
	if input.CWD == "" {
		return ProjectDecision{
			Confidence: ConfidenceUnknown, Source: SourceMissing, Reason: ReasonMissing,
		}
	}
	if !valid {
		return ProjectDecision{
			Confidence: ConfidenceUnknown, Source: SourceInvalidPath, Reason: ReasonInvalid,
		}
	}

	matches := longestRootMatches(cwd, input.Roots)
	if len(matches) > 0 {
		identities := make(map[string]ProjectRoot, len(matches))
		for _, match := range matches {
			identities[match.ProjectID] = match
		}
		if len(identities) != 1 {
			return ProjectDecision{
				Confidence: ConfidenceLow, Source: SourceConflict, Reason: ReasonConflict,
			}
		}
		for _, match := range identities {
			confidence := match.Confidence
			if confidence == "" {
				confidence = ConfidenceHigh
			}
			return ProjectDecision{
				ProjectID: match.ProjectID, DisplayName: safeProjectDisplay(match.DisplayName, match.RootPath),
				RootPath: match.RootPath, Confidence: confidence,
				Source: SourceRegisteredRoot, Reason: ReasonRootMatched,
			}
		}
	}

	projectID := projectIdentity(cwd)
	return ProjectDecision{
		ProjectID: projectID, DisplayName: safeProjectDisplay("", cwd), RootPath: cwd,
		Confidence: ConfidenceMedium, Source: SourceCWDPathDigest, Reason: ReasonPathDerived,
	}
}

func longestRootMatches(cwd string, roots []ProjectRoot) []ProjectRoot {
	longest := -1
	var matches []ProjectRoot
	for _, candidate := range roots {
		root, valid := normalizeAbsolutePath(candidate.RootPath)
		if !valid || candidate.ProjectID == "" || !pathContains(root, cwd) {
			continue
		}
		length := len(root)
		normalized := candidate
		normalized.RootPath = root
		switch {
		case length > longest:
			longest = length
			matches = []ProjectRoot{normalized}
		case length == longest:
			matches = append(matches, normalized)
		}
	}
	return matches
}

func pathContains(root, child string) bool {
	relative, err := filepath.Rel(root, child)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func normalizeAbsolutePath(value string) (string, bool) {
	if value == "" || strings.ContainsRune(value, '\x00') || !filepath.IsAbs(value) {
		return "", false
	}
	return filepath.Clean(value), true
}

func projectIdentity(path string) string {
	digest := sha256.Sum256([]byte(projectIdentityNamespace + path))
	return "local-path-v1:" + hex.EncodeToString(digest[:])
}

func safeProjectDisplay(preferred, root string) string {
	value := strings.TrimSpace(preferred)
	if value == "" {
		value = filepath.Base(root)
	}
	var builder strings.Builder
	count := 0
	for _, character := range value {
		if count >= maxProjectDisplayRunes {
			break
		}
		if unicode.IsLetter(character) || unicode.IsNumber(character) ||
			character == ' ' || character == '.' || character == '-' || character == '_' {
			builder.WriteRune(character)
			count++
		}
	}
	display := strings.TrimSpace(builder.String())
	if display != "" && display != "." && display != string(filepath.Separator) {
		return display
	}
	digest := sha256.Sum256([]byte(projectIdentityNamespace + root))
	return "Project " + strings.ToUpper(hex.EncodeToString(digest[:4]))
}
