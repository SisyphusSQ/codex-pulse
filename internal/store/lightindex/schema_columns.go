package lightindex

import (
	"strings"
)

func appendMigratedColumns(statement, before string, columns ...string) string {
	boundary := strings.Index(statement, before)
	if boundary < 0 {
		return statement
	}
	return statement[:boundary] + " " + strings.Join(columns, ", ") + "," + statement[boundary:]
}
