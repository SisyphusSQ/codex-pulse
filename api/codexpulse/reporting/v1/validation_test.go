package reportingv1

import (
	"encoding/json"
	"errors"
	"testing"
)

func validBatch() Batch {
	contribution := Contribution{ObservedAtMS: new(int64(1000)), InputTokens: new(int64(0)), TotalTokens: new(int64(0)), CostStatus: "unpriced"}
	contribution.ID = ContributionID("codex", "session", contribution, 0)
	return Batch{Version: Version, ID: "00000000-0000-0000-0000-000000000001", Sessions: []SessionSnapshot{{Provider: "codex", HomeID: "home", SessionID: "session", Revision: 1, CollectedAtMS: 2000, ProjectID: "project", Contributions: []Contribution{contribution}}}}
}

func TestStableContributionIdentitySeparatesRepeatedEventsAndIgnoresPriceMetadata(t *testing.T) {
	c := validBatch().Sessions[0].Contributions[0]
	before := ContributionID("codex", "session", c, 0)
	c.Model = new("gpt-model")
	c.CostMicroUSD = new(int64(10))
	c.PricingVersion = new("catalog-v2")
	if ContributionID("codex", "session", c, 0) != before {
		t.Fatal("price/model revision changed identity")
	}
	if ContributionID("codex", "session", c, 1) == before {
		t.Fatal("identical repeated events collided")
	}
	if ContributionID("cursor", "session", c, 0) == before {
		t.Fatal("provider identity collided")
	}
	if Key("ab", "c") == Key("a", "bc") {
		t.Fatal("concatenation ambiguity")
	}
}

func TestProtocolPreservesUnknownZeroAndLargeInteger(t *testing.T) {
	batch := validBatch()
	batch.Sessions[0].Contributions[0].TotalTokens = new(int64(9007199254740993))
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Batch
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	c := decoded.Sessions[0].Contributions[0]
	if c.OutputTokens != nil || c.InputTokens == nil || *c.InputTokens != 0 || c.TotalTokens == nil || *c.TotalTokens != 9007199254740993 {
		t.Fatal("unknown, zero or integer precision lost")
	}
}

func TestRejectsInvalidFactsVersionsAndDuplicateContributions(t *testing.T) {
	if err := validBatch().Validate(); err != nil {
		t.Fatal(err)
	}
	batch := validBatch()
	batch.Version++
	if err := batch.Validate(); !errors.Is(err, ErrVersion) {
		t.Fatal("unknown protocol accepted")
	}
	batch = validBatch()
	batch.Sessions[0].Contributions = append(batch.Sessions[0].Contributions, batch.Sessions[0].Contributions[0])
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate contribution accepted")
	}
	batch = validBatch()
	batch.Sessions[0].ProjectID = "/private/project"
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("raw project path accepted")
	}
	batch = validBatch()
	batch.Sessions[0].Contributions[0].InputTokens = new(int64(-1))
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative token accepted")
	}
}

func TestReportingMetadataDuplicatesAreRejected(t *testing.T) {
	batch := validBatch()
	batch.Accounts = []Account{{Provider: "codex", ID: "same", CollectedAtMS: 1}, {Provider: "codex", ID: "same", CollectedAtMS: 2}}
	if !errors.Is(batch.Validate(), ErrInvalid) {
		t.Fatal("duplicate accounts accepted")
	}
	batch = validBatch()
	batch.Status = []DeviceStatus{{Provider: "codex", Status: "ready"}, {Provider: "codex", Status: "partial"}}
	if !errors.Is(batch.Validate(), ErrInvalid) {
		t.Fatal("duplicate provider statuses accepted")
	}
}
