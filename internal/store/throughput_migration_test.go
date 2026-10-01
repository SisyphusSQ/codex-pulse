package store

import "testing"

func TestApplicationSchemaV35ChecksumIsFrozen(t *testing.T) {
	const want = "feb8c6acd29df72f3b2c16ef23b61cc58ff978ad676c9a5e18fe5f4ee1466935"
	if got := applicationSchemaV35Checksum(); got != want {
		t.Fatalf("v35 checksum=%s, want %s", got, want)
	}
}
