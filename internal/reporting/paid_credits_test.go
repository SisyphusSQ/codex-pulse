package reporting

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPaidCreditsCapabilityChecksAuthenticatedSyncBeforeNewFacts(t *testing.T) {
	for _, supported := range []bool{false, true} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/api/v1/sync" || r.Header.Get("Authorization") != "Bearer synthetic" {
				t.Error("capability request bypasses authenticated sync")
			}
			body := `{"code":200,"data":{"protocol_version":1}}`
			if supported {
				body = `{"code":200,"data":{"protocol_version":1,"paid_credits":true}}`
			}
			_, _ = w.Write([]byte(body))
		}))
		c, err := NewClient(s.URL, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.supportsPaidCredits(t.Context(), "synthetic")
		caps, capsErr := c.syncCapabilities(t.Context(), "synthetic")
		c.Close()
		s.Close()
		if err != nil || got != supported || capsErr != nil || caps.PaidCredits != supported || caps.MultiProviderAccountUsage {
			t.Fatalf("capability=%v caps=%+v err=%v capsErr=%v", got, caps, err, capsErr)
		}
	}
}

func TestMultiProviderAccountUsageCapabilityFromSync(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"protocol_version":1,"paid_credits":true,"multi_provider_account_usage":true}}`))
	}))
	defer s.Close()
	c, err := NewClient(s.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	caps, err := c.syncCapabilities(t.Context(), "synthetic")
	if err != nil || !caps.PaidCredits || !caps.MultiProviderAccountUsage {
		t.Fatalf("caps=%+v err=%v", caps, err)
	}
}
