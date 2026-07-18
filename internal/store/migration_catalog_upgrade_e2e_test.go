//go:build upgradee2e

package store

import "testing"

func TestUpgradeE2EMigrationCatalogIsFailClosed(t *testing.T) {
	oldLimit, oldFailure := upgradeE2ESchemaLimit, upgradeE2EFailMigration
	t.Cleanup(func() { upgradeE2ESchemaLimit, upgradeE2EFailMigration = oldLimit, oldFailure })

	upgradeE2ESchemaLimit, upgradeE2EFailMigration = "13", ""
	if got := applicationMigrationCatalog(); len(got) != 13 || got[len(got)-1].version != 13 {
		t.Fatalf("limited catalog = %#v", got)
	}
	upgradeE2ESchemaLimit = "invalid"
	if got := applicationMigrationCatalog(); got != nil {
		t.Fatalf("invalid limit returned %d migrations", len(got))
	}
	upgradeE2ESchemaLimit, upgradeE2EFailMigration = "", "99"
	if got := applicationMigrationCatalog(); got != nil {
		t.Fatalf("invalid failure target returned %d migrations", len(got))
	}
}
