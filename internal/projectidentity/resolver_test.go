package projectidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolverGroupsLinkedWorktreeUnderMainRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	main := filepath.Join(root, "repos", "codex-pulse")
	linked := filepath.Join(root, "linked", "codex-pulse")
	linkedGitDirectory := filepath.Join(main, ".git", "worktrees", "preview")
	mustMkdirAll(t, linkedGitDirectory)
	mustMkdirAll(t, filepath.Join(linked, "internal", "store"))
	mustWriteFile(t, filepath.Join(linkedGitDirectory, "commondir"), "../..\n")
	mustWriteFile(t, filepath.Join(linked, ".git"), "gitdir: "+linkedGitDirectory+"\n")

	resolver := NewResolver([]string{filepath.Join(linked, "internal", "store")})
	got := resolver.Resolve(filepath.Join(linked, "internal", "store"))
	canonicalMain, err := filepath.EvalSymlinks(main)
	if err != nil {
		t.Fatal(err)
	}
	if got.Other || got.CanonicalPath != canonicalMain {
		t.Fatalf("linked worktree resolution = %#v, want %q", got, canonicalMain)
	}
}

func TestResolverGroupsMissingCodexWorktreesByRepositoryNameOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, ".codex", "worktrees", "aaaa", "codex-pulse")
	second := filepath.Join(root, ".codex", "worktrees", "bbbb", "codex-pulse", "internal")
	resolver := NewResolver([]string{first, second})

	left := resolver.Resolve(first)
	right := resolver.Resolve(second)
	if left.Other || right.Other || left.CanonicalPath == "" || left.CanonicalPath != right.CanonicalPath {
		t.Fatalf("same-name Codex worktrees = %#v and %#v", left, right)
	}
}

func TestResolverKeepsOrdinarySameNameDirectoriesSeparate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "first", "api")
	second := filepath.Join(root, "second", "api")
	resolver := NewResolver([]string{first, second})

	left := resolver.Resolve(first)
	right := resolver.Resolve(second)
	if left.Other || right.Other || left.CanonicalPath == right.CanonicalPath {
		t.Fatalf("ordinary same-name directories were merged: %#v and %#v", left, right)
	}
}

func TestResolverClassifiesCodexDateWorkspaceAsOther(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	scratch := filepath.Join(root, "Codex", "2026-07-23", "quota-overview", "Sources")
	invalidDate := filepath.Join(root, "Codex", "2026-02-29", "real-project")
	resolver := NewResolver([]string{scratch, invalidDate})

	if got := resolver.Resolve(scratch); !got.Other || got.CanonicalPath != "" {
		t.Fatalf("scratch workspace resolution = %#v", got)
	}
	if got := resolver.Resolve(invalidDate); got.Other || got.CanonicalPath != filepath.Clean(invalidDate) {
		t.Fatalf("invalid date must remain an ordinary project: %#v", got)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
