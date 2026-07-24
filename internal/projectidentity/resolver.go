// Package projectidentity resolves local working directories into stable project
// roots before the attribution layer turns them into safe opaque identities.
package projectidentity

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Resolution keeps the canonical path private to the Go Helper. Other marks a
// Codex scratch workspace that must remain part of usage/session totals without
// appearing as a named project.
type Resolution struct {
	CanonicalPath string
	Other         bool
}

type Resolver struct {
	byPath                 map[string]Resolution
	uniqueRepositoryByName map[string]string
}

// NewResolver snapshots project identity for the supplied local working
// directories. Filesystem failures fail closed to path identity instead of
// making analytics unavailable.
func NewResolver(paths []string) Resolver {
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = normalize(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	sort.Strings(normalized)

	resolver := Resolver{
		byPath:                 make(map[string]Resolution, len(normalized)),
		uniqueRepositoryByName: make(map[string]string),
	}
	repositoriesByName := make(map[string]map[string]struct{})
	for _, path := range normalized {
		if isCodexScratchWorkspace(path) {
			resolver.byPath[path] = Resolution{Other: true}
			continue
		}
		if repository, ok := gitRepository(path); ok {
			resolver.byPath[path] = Resolution{CanonicalPath: repository}
			name := filepath.Base(repository)
			if repositoriesByName[name] == nil {
				repositoriesByName[name] = make(map[string]struct{})
			}
			repositoriesByName[name][repository] = struct{}{}
		}
	}
	for name, repositories := range repositoriesByName {
		if len(repositories) != 1 {
			continue
		}
		for repository := range repositories {
			resolver.uniqueRepositoryByName[name] = repository
		}
	}
	for _, path := range normalized {
		if _, resolved := resolver.byPath[path]; resolved {
			continue
		}
		resolver.byPath[path] = resolver.resolveWithoutGit(path)
	}
	return resolver
}

func (resolver Resolver) Resolve(path string) Resolution {
	path = normalize(path)
	if resolution, exists := resolver.byPath[path]; exists {
		return resolution
	}
	if isCodexScratchWorkspace(path) {
		return Resolution{Other: true}
	}
	if repository, ok := gitRepository(path); ok {
		return Resolution{CanonicalPath: repository}
	}
	return resolver.resolveWithoutGit(path)
}

func (resolver Resolver) resolveWithoutGit(path string) Resolution {
	if _, name, groupedPath, ok := codexManagedWorktree(path); ok {
		if repository := resolver.uniqueRepositoryByName[name]; repository != "" {
			return Resolution{CanonicalPath: repository}
		}
		return Resolution{CanonicalPath: groupedPath}
	}
	return Resolution{CanonicalPath: path}
}

func normalize(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func gitRepository(path string) (string, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	candidate := path
	if !info.IsDir() {
		candidate = filepath.Dir(candidate)
	}
	for {
		if repository, ok := repositoryAt(candidate); ok {
			return repository, true
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", false
		}
		candidate = parent
	}
}

func repositoryAt(workingTree string) (string, bool) {
	dotGit := filepath.Join(workingTree, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return canonicalExistingPath(workingTree), true
	}
	gitDirectoryValue, ok := firstLine(dotGit)
	if !ok || !strings.HasPrefix(gitDirectoryValue, "gitdir:") {
		return "", false
	}
	gitDirectory := strings.TrimSpace(strings.TrimPrefix(gitDirectoryValue, "gitdir:"))
	if gitDirectory == "" {
		return "", false
	}
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(workingTree, gitDirectory)
	}
	gitDirectory = filepath.Clean(gitDirectory)
	if info, err = os.Stat(gitDirectory); err != nil || !info.IsDir() {
		return "", false
	}

	commonGitDirectory := gitDirectory
	if commonDirectoryValue, found := firstLine(filepath.Join(gitDirectory, "commondir")); found {
		commonDirectoryValue = strings.TrimSpace(commonDirectoryValue)
		if commonDirectoryValue == "" {
			return "", false
		}
		if filepath.IsAbs(commonDirectoryValue) {
			commonGitDirectory = commonDirectoryValue
		} else {
			commonGitDirectory = filepath.Join(gitDirectory, commonDirectoryValue)
		}
	}
	commonGitDirectory = canonicalExistingPath(commonGitDirectory)
	if filepath.Base(commonGitDirectory) == ".git" {
		return filepath.Dir(commonGitDirectory), true
	}
	return canonicalExistingPath(workingTree), true
}

func canonicalExistingPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}

func firstLine(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 256), 4_096)
	if !scanner.Scan() {
		return "", false
	}
	return scanner.Text(), scanner.Err() == nil
}

func codexManagedWorktree(path string) (root, name, groupedPath string, ok bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", "", "", false
	}
	components := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index := len(components) - 4; index >= 0; index-- {
		if components[index] != ".codex" || components[index+1] != "worktrees" ||
			components[index+2] == "" || components[index+3] == "" {
			continue
		}
		root = filepath.FromSlash(strings.Join(components[:index+4], "/"))
		name = components[index+3]
		container := filepath.FromSlash(strings.Join(components[:index+2], "/"))
		groupedPath = filepath.Join(container, "_grouped", name)
		return filepath.Clean(root), name, filepath.Clean(groupedPath), true
	}
	return "", "", "", false
}

func isCodexScratchWorkspace(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	components := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index := 0; index+2 < len(components); index++ {
		if components[index] == "Codex" && validDateComponent(components[index+1]) &&
			components[index+2] != "" {
			return true
		}
	}
	return false
}

func validDateComponent(value string) bool {
	if len(value) != len("2006-01-02") || value[4] != '-' || value[7] != '-' {
		return false
	}
	for index, character := range value {
		if index == 4 || index == 7 {
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}
