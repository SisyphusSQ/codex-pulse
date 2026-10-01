package gormv2

import (
	"errors"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/requestinfo"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm/logger"
	"strings"
	"testing"
	"time"
)

func TestDatabaseLogCarriesRequestIDWithoutSQLValues(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	original := log.Logger
	log.Logger = log.NewZapLogger(zap.New(core).Sugar())
	t.Cleanup(func() { log.Logger = original })
	q := queryLogger{level: logger.Info}
	q.Trace(requestinfo.WithID(t.Context(), "test-request"), time.Now(), func() (string, int64) { return "SELECT sensitive_value", 0 }, errors.New("sensitive_value"))
	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].ContextMap()["request_id"] != "test-request" {
		t.Fatal("request association lost")
	}
	if strings.Contains(entries[0].Message, "sensitive_value") || entries[0].ContextMap()["sql"] != nil {
		t.Fatal("query values leaked")
	}
}
