package privacy

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type DatabaseReport struct {
	Tables  int
	Strings int64
	Blobs   int64
}

// InspectDatabase audits schema field names and text values through GORM. It
// allows product-required private path metadata, but rejects body/credential
// envelopes. Findings never contain table, column or row values.
func InspectDatabase(ctx context.Context, database *storesqlite.Store) (DatabaseReport, error) {
	report := DatabaseReport{}
	err := database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		tables, err := connection.WithContext(ctx).Migrator().GetTables()
		if err != nil {
			return err
		}
		sort.Strings(tables)
		for _, table := range tables {
			if !safeIdentifier.MatchString(table) {
				return &Violation{Finding: FindingForbiddenField}
			}
			report.Tables++
			columns, err := connection.WithContext(ctx).Migrator().ColumnTypes(table)
			if err != nil {
				return err
			}
			for _, column := range columns {
				if err := InspectField(column.Name()); err != nil {
					return err
				}
				if !safeIdentifier.MatchString(column.Name()) {
					continue
				}
				if !isTextColumn(column.DatabaseTypeName()) && !isBinaryColumn(column.DatabaseTypeName()) {
					continue
				}
				rows, err := connection.WithContext(ctx).Table(table).Select(column.Name()).Rows()
				if err != nil {
					return err
				}
				if err := inspectRows(rows, &report, isBinaryColumn(column.DatabaseTypeName())); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return report, err
}

func inspectRows(rows *sql.Rows, report *DatabaseReport, binary bool) error {
	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			_ = rows.Close()
			return err
		}
		if value != nil {
			if binary {
				report.Blobs++
			} else {
				report.Strings++
			}
			if ContainsSensitiveEnvelope(string(value)) {
				_ = rows.Close()
				return &Violation{Finding: FindingSensitiveValue}
			}
		}
	}
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	return closeErr
}

func isTextColumn(databaseType string) bool {
	typeName := strings.ToUpper(databaseType)
	return strings.Contains(typeName, "CHAR") || strings.Contains(typeName, "CLOB") || strings.Contains(typeName, "TEXT")
}

func isBinaryColumn(databaseType string) bool {
	return strings.Contains(strings.ToUpper(databaseType), "BLOB")
}
