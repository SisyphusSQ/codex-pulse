package core

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/reporting"
)

func TestReportingRPCStatusPreservesZeroPresenceAndExcludesSecrets(t *testing.T) {
	dto := reporting.Status{Endpoint: "https://center.example", ClientID: "device", Enabled: false, State: "disabled", IntervalSeconds: 60, PendingBatches: 0, LastSuccessAtMS: new(int64(0))}
	out := &corev1.ReportingStatusResponse{}
	if err := EncodeResponse(dto, out); err != nil {
		t.Fatal(err)
	}
	if out.LastSuccessAtMs == nil || *out.LastSuccessAtMs != 0 || out.LastAttemptAtMs != nil {
		t.Fatal("zero or unknown presence lost")
	}
	bytes, err := protojson.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential", "code", "cookie", "secret", "session_body", "raw_jsonl"} {
		if strings.Contains(string(bytes), forbidden) {
			t.Fatal("secret in status")
		}
	}
}
