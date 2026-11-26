package opensheet_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/opensheet"
)

func newControl(t *testing.T, mux *http.ServeMux) *opensheet.Control {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := opensheet.NewControl(opensheet.ControlConfig{BaseURL: srv.URL, Token: "tok", AllowPrivateHosts: true, Retry: noRetry()})
	require.NoError(t, err)
	return c
}

func TestControl_ListProjects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/project.v1.ProjectService/ListProjects", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		writeJSON(t, w, http.StatusOK, map[string]any{"projects": []map[string]any{
			{"id": "p1", "orgId": "o1", "orgSlug": "acme", "slug": "home", "name": "Home"},
		}})
	})
	got, err := newControl(t, mux).ListProjects(t.Context())
	require.NoError(t, err)
	require.Equal(t, []opensheet.Project{{ID: "p1", OrgID: "o1", OrgSlug: "acme", Slug: "home", Name: "Home"}}, got)
}

func TestControl_ListCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/credential.v1.CredentialService/List", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "p1", in["projectId"])
		writeJSON(t, w, http.StatusOK, map[string]any{"credentials": []map[string]any{
			{"id": "c1", "projectId": "p1", "name": "Ana", "kind": "google_oauth", "status": "active", "googleAccountEmail": "ana@gmail.com"},
		}})
	})
	got, err := newControl(t, mux).ListCredentials(t.Context(), "p1")
	require.NoError(t, err)
	require.Equal(t, []opensheet.Credential{{ID: "c1", ProjectID: "p1", Name: "Ana", Kind: "google_oauth", Status: "active", GoogleAccountEmail: "ana@gmail.com"}}, got)
}

func TestControl_Provision(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/spreadsheet.v1.SpreadsheetService/Provision", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "p1", in["projectId"])
		require.Equal(t, "c1", in["credentialId"])
		require.Equal(t, "Home records", in["title"])
		require.Equal(t, "k1", in["idempotencyKey"])
		require.Equal(t, []any{map[string]any{"title": "Wallets", "headers": []any{"id", "name"}, "slug": "w"}}, in["tabs"])
		writeJSON(t, w, http.StatusOK, map[string]any{
			"spreadsheet": map[string]any{"id": "s1", "title": "Home records", "googleFileId": "g1"},
			"sheets":      []map[string]any{{"slug": "w"}},
		})
	})
	got, err := newControl(t, mux).Provision(t.Context(), opensheet.ProvisionRequest{
		ProjectID: "p1", CredentialID: "c1", Title: "Home records", IdempotencyKey: "k1",
		Tabs: []opensheet.ProvisionTab{{Title: "Wallets", Headers: []string{"id", "name"}, Slug: "w"}},
	})
	require.NoError(t, err)
	require.Equal(t, opensheet.Spreadsheet{ID: "s1", Title: "Home records", GoogleFileID: "g1"}, got)
}

func TestControl_ConnectErrorCarriesCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/project.v1.ProjectService/ListProjects", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "unauthenticated", "message": "who"})
	})
	_, err := newControl(t, mux).ListProjects(t.Context())
	ce, ok := opensheet.AsConnectError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, "unauthenticated", ce.Code)
	require.True(t, ce.Refused())
}

func TestNewControl_RefusesPrivateLiteralHost(t *testing.T) {
	_, err := opensheet.NewControl(opensheet.ControlConfig{BaseURL: "http://127.0.0.1:1", Token: "t"})
	require.Error(t, err)
}
