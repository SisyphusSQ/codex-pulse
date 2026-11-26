package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func agyRegistryObjects() []storeschema.Object {
	return []storeschema.Object{
		{ObjectType: "table", Name: "agent_provider_snapshots", Statement: strings.ReplaceAll(currentAgentProviderSnapshotStatement(), grokProviderCheck, "('codex','cursor','grok','dsh','agy')")},
		{ObjectType: "table", Name: "agent_provider_sources", Statement: strings.ReplaceAll(currentAgentProviderSourceStatement(), grokProviderCheck, "('codex','cursor','grok','dsh','agy')")},
	}
}

func currentAGYProviderSchemaObjects() []storeschema.Object {
	objects := currentDSHProviderSchemaObjects()
	for i := range objects {
		for _, registry := range agyRegistryObjects() {
			if objects[i].Name == registry.Name {
				objects[i] = registry
			}
		}
	}
	return append(objects, agyProviderSchemaObjects...)
}

func applicationSchemaV40Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, 40, "agy-local-facts")
	for _, object := range append(agyRegistryObjects(), agyProviderSchemaObjects...) {
		_, _ = fmt.Fprintln(hasher, object.ObjectType, object.Name, strings.TrimSpace(storeschema.NormalizeSQL(storeschema.CanonicalSQL(object.Statement))))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func migrateAGYForV40(ctx context.Context, transaction *gorm.DB) error {
	database := transaction.WithContext(ctx)
	// 固定 DDL 仅扩大客户端白名单并建立独立 agy_* 事实表；既有事实与历史 migration checksum 不变。
	for _, object := range agyRegistryObjects() {
		if err := database.Exec("ALTER TABLE " + object.Name + " RENAME TO " + object.Name + "_v39").Error; err != nil {
			return err
		}
		if err := storeschema.EnsureObjects(ctx, database, []storeschema.Object{object}); err != nil {
			return err
		}
		if err := database.Exec("INSERT INTO " + object.Name + " SELECT * FROM " + object.Name + "_v39").Error; err != nil {
			return err
		}
		if err := database.Exec("DROP TABLE " + object.Name + "_v39").Error; err != nil {
			return err
		}
	}
	return storeschema.EnsureObjects(ctx, database, agyProviderSchemaObjects)
}
