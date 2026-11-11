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
		c.Close()
		s.Close()
		if err != nil || got != supported {
			t.Fatalf("capability=%v err=%v", got, err)
		}
	}
}
