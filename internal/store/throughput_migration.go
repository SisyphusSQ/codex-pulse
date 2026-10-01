package store

import (
	"crypto/sha256"
	"fmt"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func applicationSchemaV35Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV35Version, "lightweight-session-throughput")
	for _, object := range storelight.ThroughputSchemaObjects() {
		_, _ = fmt.Fprintln(hasher, object.ObjectType, object.Name, object.Statement)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}
