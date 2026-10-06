package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func dshRegistryObjects() []storeschema.Object {
	return []storeschema.Object{
		{ObjectType: "table", Name: "agent_provider_snapshots", Statement: strings.ReplaceAll(currentAgentProviderSnapshotStatement(), grokProviderCheck, "('codex','cursor','grok','dsh')")},
		{ObjectType: "table", Name: "agent_provider_sources", Statement: strings.ReplaceAll(currentAgentProviderSourceStatement(), grokProviderCheck, "('codex','cursor','grok','dsh')")},
	}
}

func currentDSHProviderSchemaObjects() []storeschema.Object {
	objects := currentProviderSchemaObjects()
	for i := range objects {
		for _, registry := range dshRegistryObjects() {
			if objects[i].Name == registry.Name {
				objects[i] = registry
			}
		}
	}
	return append(objects, currentDSHTitleSchemaObjects()...)
}

func applicationSchemaV36Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, 36, "dsh-local-facts")
	for _, object := range append(dshRegistryObjects(), dshProviderSchemaObjects...) {
		_, _ = fmt.Fprintln(hasher, object.ObjectType, object.Name, strings.TrimSpace(storeschema.NormalizeSQL(storeschema.CanonicalSQL(object.Statement))))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func migrateDSHForV36(ctx context.Context, transaction *gorm.DB) error {
	database := transaction.WithContext(ctx)
	// 固定 DDL 仅扩大客户端白名单；既有事实与旧 migration checksum 不变。
	for _, object := range dshRegistryObjects() {
		if err := database.Exec("ALTER TABLE " + object.Name + " RENAME TO " + object.Name + "_v35").Error; err != nil {
			return err
		}
		if err := storeschema.EnsureObjects(ctx, database, []storeschema.Object{object}); err != nil {
			return err
		}
		if err := database.Exec("INSERT INTO " + object.Name + " SELECT * FROM " + object.Name + "_v35").Error; err != nil {
			return err
		}
		if err := database.Exec("DROP TABLE " + object.Name + "_v35").Error; err != nil {
			return err
		}
	}
	return storeschema.EnsureObjects(ctx, database, dshProviderSchemaObjects)
}
