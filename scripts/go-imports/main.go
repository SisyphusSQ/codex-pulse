// go-imports 固定标准库、第三方、当前仓库的导入顺序，不安装外部格式工具。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const project = "github.com/SisyphusSQ/codex-pulse"

type imported struct {
	path       string
	source     string
	group      int
	start, end int
}
type replacement struct {
	start, end int
	source     []byte
}

func group(path string) int {
	if path == project || strings.HasPrefix(path, project+"/") {
		return 2
	}
	first, _, _ := strings.Cut(path, "/")
	if strings.Contains(first, ".") {
		return 1
	}
	return 0
}

func arrange(filename string, source []byte) ([]byte, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, filename, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var edits []replacement
	for _, decl := range file.Decls {
		imports, ok := decl.(*ast.GenDecl)
		if !ok || imports.Tok != token.IMPORT || !imports.Lparen.IsValid() {
			continue
		}
		var specs []imported
		cgo := false
		for _, spec := range imports.Specs {
			i := spec.(*ast.ImportSpec)
			path, err := strconv.Unquote(i.Path.Value)
			if err != nil {
				return nil, err
			}
			if path == "C" {
				cgo = true
			}
			start, end := set.Position(i.Pos()).Offset, set.Position(i.End()).Offset
			if i.Doc != nil {
				start = set.Position(i.Doc.Pos()).Offset
			}
			if i.Comment != nil {
				end = set.Position(i.Comment.End()).Offset
			}
			specs = append(specs, imported{path: path, source: strings.TrimSpace(string(source[start:end])), group: group(path), start: start, end: end})
		}
		if cgo {
			continue
		} // cgo 前导指令不能移动或并入其他块。
		start, end := set.Position(imports.Pos()).Offset, set.Position(imports.End()).Offset
		for _, comment := range file.Comments {
			position := set.Position(comment.Pos()).Offset
			if position <= start || position >= end {
				continue
			}
			if !slices.ContainsFunc(specs, func(i imported) bool { return position >= i.start && position < i.end }) {
				return nil, fmt.Errorf("%s: import block has unattached comment; group manually", filename)
			}
		}
		slices.SortStableFunc(specs, func(a, b imported) int {
			if a.group != b.group {
				return a.group - b.group
			}
			return strings.Compare(a.path, b.path)
		})
		var block strings.Builder
		block.WriteString("import (\n")
		last := -1
		for _, i := range specs {
			if last != -1 && last != i.group {
				block.WriteString("\n")
			}
			for line := range strings.SplitSeq(i.source, "\n") {
				block.WriteString("\t" + strings.TrimSpace(line) + "\n")
			}
			last = i.group
		}
		block.WriteString(")")
		edits = append(edits, replacement{start: start, end: end, source: []byte(block.String())})
	}
	for index := len(edits) - 1; index >= 0; index-- {
		e := edits[index]
		source = append(append(append([]byte(nil), source[:e.start]...), e.source...), source[e.end:]...)
	}
	return format.Source(source)
}

func main() {
	write := flag.Bool("write", false, "write grouped imports")
	check := flag.Bool("check", false, "reject ungrouped imports")
	flag.Parse()
	if *write == *check {
		fmt.Fprintln(os.Stderr, "use exactly one of --write or --check")
		os.Exit(2)
	}
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	changed, failed := 0, false
	visit := func(path string) {
		source, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			return
		}
		formatted, err := arrange(path, source)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			return
		}
		if bytes.Equal(source, formatted) {
			return
		}
		changed++
		if *check {
			fmt.Fprintln(os.Stderr, path)
			failed = true
			return
		}
		stat, err := os.Stat(path)
		if err == nil {
			err = os.WriteFile(path, formatted, stat.Mode().Perm())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			continue
		}
		if !stat.IsDir() {
			visit(path)
			continue
		}
		err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if slices.Contains([]string{".git", ".artifacts", ".build", ".cache", "node_modules", "vendor", "bin"}, entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") {
				visit(path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("Go import groups: %d file(s) changed\n", changed)
}
