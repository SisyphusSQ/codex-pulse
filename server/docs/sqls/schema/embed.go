// Package schema 提供运行时使用的唯一 SQL 结构事实源。
package schema

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
)

// Version 只在已交付结构发生有意升级时递增。
const Version int64 = 5

//go:embed center_mysql.sql center_sqlite.sql v3_mysql.sql v3_sqlite.sql v4_mysql.sql v4_sqlite.sql v5_mysql.sql v5_sqlite.sql
var files embed.FS

// Definition 返回受控 dialect 的 SQL、摘要以及从 SQL 提取的表字段清单。
func Definition(driver string) ([]string, string, map[string][]string, error) {
	if driver != "mysql" && driver != "sqlite" {
		return nil, "", nil, fmt.Errorf("unsupported schema driver")
	}
	content, err := files.ReadFile("center_" + driver + ".sql")
	if err != nil {
		return nil, "", nil, err
	}
	addition, err := files.ReadFile("v3_" + driver + ".sql")
	if err != nil {
		return nil, "", nil, err
	}
	content = append(content, addition...)
	addition, err = files.ReadFile("v4_" + driver + ".sql")
	if err != nil {
		return nil, "", nil, err
	}
	content = append(content, addition...)
	addition, err = files.ReadFile("v5_" + driver + ".sql")
	if err != nil {
		return nil, "", nil, err
	}
	content = append(content, addition...)
	sum := sha256.Sum256(content)
	var lines []string
	columns := make(map[string][]string)
	table := ""
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, "CREATE TABLE IF NOT EXISTS ") {
			table = strings.Fields(line)[5]
			columns[table] = nil
		} else if strings.HasPrefix(line, ")") {
			table = ""
		} else if table != "" {
			name := strings.Fields(line)[0]
			if name != "PRIMARY" && name != "UNIQUE" && name != "KEY" && name != "CONSTRAINT" && name != "CHECK" {
				columns[table] = append(columns[table], name)
			}
		}
	}
	var statements []string
	for statement := range strings.SplitSeq(strings.Join(lines, "\n"), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements, hex.EncodeToString(sum[:]), columns, nil
}
