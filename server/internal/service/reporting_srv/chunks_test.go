package reporting_srv

import (
	"errors"
	"testing"
	"uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestChunkedSnapshotAtomicReplayAndCorruption(t *testing.T) {
	s, db, clients := reportingFixture(t)
	full := centerfixture.Snapshot()
	centerfixture.SendSnapshot(t, s, clients[0], full)
	full.Revision = 2
	full.CollectedAtMS = 4000
	for index := range 20032 {
		full.Contributions = append(full.Contributions, centerfixture.Contribution(1, int64(index+2000)))
	}
	for index := range 16691 {
		i := reportingv1.Invocation{ObservedAtMS: int64(index), Kind: "tool", Name: "exec_command", Outcome: "unknown"}
		i.ID = reportingv1.InvocationID(full.Provider, full.SessionID, i, 0)
		full.Invocations = append(full.Invocations, i)
	}
	parts, err := reportingv1.SplitSnapshot(full)
	if err != nil {
		t.Fatal(err)
	}
	// Reverse arrival and replay must retain the old projection until complete.
	for index := len(parts) - 1; index >= 0; index-- {
		batch := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{parts[index]}}
		first, err := s.Accept(t.Context(), clients[0], batch)
		if err != nil {
			t.Fatal(index, err)
		}
		again, err := s.Accept(t.Context(), clients[0], batch)
		if err != nil || first != again {
			t.Fatal("replay", err)
		}
		_, total := readSession(t, db)
		if index > 0 && total != 100 {
			t.Fatal("partial projection exposed", index, total)
		}
		if index == 0 && total != 20132 {
			t.Fatal("complete total", total)
		}
	}
	var count int64
	if err := db.Model(&reporting_do.Invocation{}).Count(&count).Error; err != nil || count != 16691 {
		t.Fatal("invocations lost", count, err)
	}
	if err := db.Model(&reporting_do.SnapshotChunk{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("completed staging retained", count, err)
	}
	full.Revision = 3
	parts, err = reportingv1.SplitSnapshot(full)
	if err != nil {
		t.Fatal(err)
	}
	parts[0].Contributions[0].TotalTokens = new(int64(101))
	for index, part := range parts {
		_, err := s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{part}})
		if index < len(parts)-1 && err != nil {
			t.Fatal(err)
		}
		if index == len(parts)-1 && !errors.Is(err, utils.ErrBadParamInput) {
			t.Fatal("corrupt digest accepted", err)
		}
	}
	_, total := readSession(t, db)
	if total != 20132 {
		t.Fatal("corruption changed accepted data")
	}
}
