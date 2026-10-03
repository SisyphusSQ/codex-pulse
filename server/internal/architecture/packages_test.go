package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
)

func TestBusinessPackagesAndTableModelsFollowStarterContract(t *testing.T) {
	root := filepath.Clean("../..")
	base := "github.com/SisyphusSQ/codex-pulse/server/internal/"
	for _, layer := range []string{"controller", "service", "repository"} {
		entries, err := os.ReadDir(filepath.Join(root, "internal", layer))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && entry.Name() != "module.go" {
				t.Errorf("%s 根包应仅装配：%s", layer, entry.Name())
			}
		}
	}
	_, _, tables, err := schema.Definition("mysql")
	if err != nil {
		t.Fatal(err)
	}
	mapped := map[string]string{}
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "internal/models/") && !strings.HasPrefix(rel, "internal/service/") && !strings.HasPrefix(rel, "internal/repository/") && !strings.HasPrefix(rel, "internal/controller/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		layer := strings.Split(rel, "/")[1]
		parts := strings.Split(rel, "/")
		domain := filepath.Base(filepath.Dir(path))
		if layer != "models" && len(parts) > 3 {
			suffix := map[string]string{"service": "_srv", "repository": "_repo", "controller": "_controller"}[layer]
			if !strings.HasSuffix(domain, suffix) || file.Name.Name != domain {
				t.Errorf("业务子包名称不符：%s", rel)
			}
			for _, imp := range file.Imports {
				target, _ := strconv.Unquote(imp.Path.Value)
				if target == base+layer {
					t.Errorf("业务子包反向依赖装配根：%s", rel)
				}
			}
		}
		if strings.HasPrefix(rel, "internal/models/") {
			for _, imp := range file.Imports {
				target, _ := strconv.Unquote(imp.Path.Value)
				for _, disallowed := range []string{"service", "controller", "repository", "lib/gorm"} {
					if strings.HasPrefix(target, base+disallowed) {
						t.Errorf("模型反向依赖业务/连接：%s", rel)
					}
				}
			}
			if strings.HasPrefix(rel, "internal/models/dto/") && !strings.HasSuffix(domain, "_dto") {
				t.Errorf("DTO 未分域：%s", rel)
			}
		}
		if !strings.HasPrefix(rel, "internal/models/do/mysql/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(domain, "_do") || file.Name.Name != domain {
			t.Errorf("DO 未分域：%s", rel)
		}
		methods := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "TableName" || fn.Recv == nil {
				continue
			}
			methods++
			if fn.Body == nil || len(fn.Body.List) != 1 {
				t.Errorf("表名必须为固定映射：%s", rel)
				continue
			}
			ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				t.Errorf("表名映射无效：%s", rel)
				continue
			}
			literal, ok := ret.Results[0].(*ast.BasicLit)
			if !ok {
				t.Errorf("表名不是常量：%s", rel)
				continue
			}
			name, err := strconv.Unquote(literal.Value)
			if err != nil {
				return err
			}
			if earlier := mapped[name]; earlier != "" {
				t.Errorf("重复表 DO：%s / %s", earlier, rel)
			}
			mapped[name] = rel
			if _, ok := tables[name]; !ok {
				t.Errorf("DO 没有权威 DDL：%s", rel)
			}
		}
		if methods != 1 {
			t.Errorf("一表一文件要求一个 TableName，实际 %d：%s", methods, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for table := range tables {
		if mapped[table] == "" {
			t.Errorf("权威表没有独立 DO：%s", table)
		}
	}
}
