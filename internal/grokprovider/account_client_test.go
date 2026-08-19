package grokprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountClientReadsUserSubscriptionProfile(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/user" || request.URL.Query().Get("include") != "subscription" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get("x-userid") != "user-1" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"email": "person@example.com",
			"principalType": "User",
			"subscriptionTier": "GrokPro"
		}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewAccountClient(AccountClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(),
		TokenSource: staticTokenSource{token: AccessToken{Token: "secret", UserID: "user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := client.GetAccount(context.Background())
	if err != nil || account.Email != "person@example.com" || account.PrincipalType != "User" ||
		account.Subscription != "GrokPro" {
		t.Fatalf("account = %#v, %v", account, err)
	}
}
