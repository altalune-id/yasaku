package opensheetsync

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEndpoint_ClientRefusesAPrivateHostUnlessAllowed(t *testing.T) {
	e := Endpoint{BaseURL: "http://10.0.0.7:8080"}
	_, err := e.client("acme", "home", "osk_live_secret")
	require.Error(t, err, "a private host is refused by default")

	e.AllowPrivateHosts = true
	_, err = e.client("acme", "home", "osk_live_secret")
	require.NoError(t, err)
}

func TestEndpoint_ClientNeedsTheOpensheetOrgAndProject(t *testing.T) {
	e := Endpoint{BaseURL: "https://opensheet.example.com"}
	_, err := e.client("", "home", "osk_live_secret")
	require.Error(t, err)
	_, err = e.client("acme", "", "osk_live_secret")
	require.Error(t, err)
}

func TestEndpoint_ClientSendsTheKeyToTheProjectAndTriesOnce(t *testing.T) {
	var hits atomic.Int32
	var auth, path, agent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		path.Store(r.URL.Path)
		agent.Store(r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	c, err := Endpoint{BaseURL: srv.URL, AllowPrivateHosts: true}.client("acme", "budget", "osk_live_other")
	require.NoError(t, err)
	_, err = c.Capabilities(t.Context(), "wallets")
	require.Error(t, err)
	require.EqualValues(t, 1, hits.Load(), "one attempt: the queue retries the job, and inline runs keep their budget")
	require.Equal(t, "Bearer osk_live_other", auth.Load())
	require.Equal(t, "/api/v1/orgs/acme/projects/budget/sheets/wallets/capabilities", path.Load())
	require.Equal(t, "yasaku-opensheetsync/1", agent.Load())
}

func TestEndpoint_ConfigDefaultsTheTimeoutToEightSeconds(t *testing.T) {
	require.Equal(t, 8*time.Second, Endpoint{BaseURL: "https://opensheet.example.com"}.config("acme", "home", "k").Timeout)
	require.Equal(t, 2*time.Second, Endpoint{BaseURL: "https://opensheet.example.com", Timeout: 2 * time.Second}.config("acme", "home", "k").Timeout)
}

func TestEndpoint_ValidateRejectsWhatEveryClientWouldReject(t *testing.T) {
	tests := []struct {
		name    string
		e       Endpoint
		wantErr bool
	}{
		{"public https", Endpoint{BaseURL: "https://opensheet.example.com"}, false},
		{"private literal IP", Endpoint{BaseURL: "https://10.0.0.5"}, true},
		{"loopback", Endpoint{BaseURL: "http://127.0.0.1:9"}, true},
		{"private literal IP allowed", Endpoint{BaseURL: "https://10.0.0.5", AllowPrivateHosts: true}, false},
		{"no scheme", Endpoint{BaseURL: "opensheet.example.com"}, true},
		{"empty", Endpoint{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.e.Validate()
			require.Equal(t, tt.wantErr, err != nil, "%v", err)
			if err == nil {
				return
			}
			_, cErr := tt.e.client("acme", "home", "osk_live_secret")
			require.Error(t, cErr, "Validate refuses only what a client refuses")
		})
	}
}

func TestEndpoint_ValidateNeverPrintsURLCredentials(t *testing.T) {
	err := Endpoint{BaseURL: "https://user:hunter2@10.0.0.5"}.Validate()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2")
}
