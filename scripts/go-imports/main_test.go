package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRegroupPreservesAliasesCommentsAndImportIdentity(t *testing.T) {
	source := []byte("package sample\nimport (\n // project dependency\n alias \"github.com/SisyphusSQ/codex-pulse/internal/core\"\n \"net/http\" // standard dependency\n _ \"github.com/libtnb/sqlite\"\n)\n")
	formatted, err := arrange("sample.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte("// project dependency")) || !bytes.Contains(formatted, []byte("// standard dependency")) {
		t.Fatal("comments lost")
	}
	std, third, project := strings.Index(string(formatted), "\"net/http\""), strings.Index(string(formatted), "\"github.com/libtnb/sqlite\""), strings.Index(string(formatted), "alias \"github.com/SisyphusSQ/codex-pulse/internal/core\"")
	if !(std < third && third < project) {
		t.Fatal("group order wrong")
	}
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", formatted, parser.ParseComments)
	if err != nil || len(file.Imports) != 3 {
		t.Fatalf("parsed imports: %v", err)
	}
	if file.Imports[1].Name.Name != "_" || file.Imports[2].Name.Name != "alias" {
		t.Fatal("alias semantics changed")
	}
	again, err := arrange("sample.go", formatted)
	if err != nil || !bytes.Equal(again, formatted) {
		t.Fatal("formatter not idempotent")
	}
}

func TestUnattachedImportCommentsFailWithoutDroppingText(t *testing.T) {
	source := []byte("package sample\nimport (\n // free comment\n\n \"net/http\"\n)\n")
	if _, err := arrange("sample.go", source); err == nil {
		t.Fatal("unattached comment would be lost")
	}
}
