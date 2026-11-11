package creditbalance

import (
	"encoding/json"
	"testing"
)

func TestDecodeOfficialBalancePreservesExactZeroAndRejectsInvalidFields(t *testing.T) {
	for _, test := range []struct{ raw, state, amount string }{
		{`{"balance":"62500.0000000000","hasCredits":true,"unlimited":false}`, "known", "62500"},
		{`{"balance":"0.000000000000000001","hasCredits":true,"unlimited":false}`, "known", "0.000000000000000001"},
		{`{"balance":"0","hasCredits":false,"unlimited":false}`, "known", "0"},
		{`null`, "unavailable", ""},
		{`{"hasCredits":true,"unlimited":true}`, "known", ""},
		{`{"balance":62500,"hasCredits":true,"unlimited":false}`, "invalid", ""},
		{`{"balance":"-1","hasCredits":true,"unlimited":false}`, "invalid", ""},
		{`{"balance":"NaN","hasCredits":true,"unlimited":false}`, "invalid", ""},
		{`{"balance":"1e10000","hasCredits":true,"unlimited":false}`, "invalid", ""},
		{`{"balance":"1","hasCredits":true}`, "invalid", ""},
		{`{"balance":"1","balance":"2","hasCredits":true,"unlimited":false}`, "invalid", ""},
	} {
		s := Decode(json.RawMessage(test.raw), "codex")
		if !s.Valid() || s.State != test.state || test.amount != "" && (s.Balance == nil || *s.Balance != test.amount) {
			t.Fatalf("Decode(%s)=%+v", test.raw, s)
		}
	}
}

func TestViewsUsesLatestBalanceWithoutInventingConsumption(t *testing.T) {
	const now = 40 * DayMS
	makeSample := func(amount string, at int64) Snapshot {
		s := Decode(json.RawMessage(`{"balance":"`+amount+`","hasCredits":true,"unlimited":false}`), "codex")
		s.ObservedAtMS = at
		return s
	}
	samples := []Snapshot{makeSample("100", now-2*DayMS), makeSample("90", now-100), makeSample("120", now-50), {LimitID: "codex", State: "invalid", ObservedAtMS: now}}
	v := Views(samples, now, now, true)[0]
	if v.Balance == nil || *v.Balance != "120" || v.Freshness != "stale" || v.ConsumptionState != "source_unavailable" || len(v.History) != 2 {
		t.Fatalf("view=%+v", v)
	}
	samples = append(samples, makeSample("0", now+1))
	if *Views(samples, now, now, false)[0].Balance != "120" {
		t.Fatal("future balance selected")
	}
	conflict := Views([]Snapshot{makeSample("1", now), makeSample("2", now)}, now, now, false)[0]
	if conflict.Freshness != "conflict" || conflict.Balance != nil || len(conflict.History) != 1 || conflict.History[0].Balance != nil {
		t.Fatalf("conflicting balances selected: %+v", conflict)
	}
}
