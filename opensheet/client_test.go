package opensheet_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/opensheet"
)

func noRetry() httpclient.RetryPolicy {
	return httpclient.RetryPolicy{MaxAttempts: 1}
}

func newTestClient(t *testing.T, baseURL string) *opensheet.Client {
	t.Helper()
	c, err := opensheet.New(opensheet.Config{
		BaseURL:           baseURL,
		Org:               "o1",
		Project:           "p1",
		Token:             "t",
		AllowPrivateHosts: true,
		Retry:             noRetry(),
	})
	require.NoError(t, err)
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func writeErrorEnvelope(t *testing.T, w http.ResponseWriter, status int, code, msg string) {
	t.Helper()
	writeJSON(t, w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func TestPages_FollowsLinkAndReportsStale(t *testing.T) {
	var authHeaders []string
	var page1Query string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/o1/projects/p1/sheets/s1", func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if r.URL.Query().Get("cursor") == "" {
			page1Query = r.URL.RawQuery
			w.Header().Set("ETag", `"e1"`)
			w.Header().Set("Link", fmt.Sprintf(`<%s?cursor=abc2>; rel="next"`, r.URL.Path))
			writeJSON(t, w, http.StatusOK, []opensheet.Row{{"id": "1"}})
			return
		}
		w.Header().Set("X-Opensheet-Stale", "true")
		writeJSON(t, w, http.StatusOK, []opensheet.Row{{"id": "2"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	var pages []opensheet.Page
	for page, err := range c.Pages(t.Context(), "s1", opensheet.Query{
		Where: []opensheet.Filter{{Column: "amount", Op: "num.gte", Value: "100"}},
		Limit: 2,
	}) {
		require.NoError(t, err)
		pages = append(pages, page)
	}

	require.Len(t, pages, 2)
	require.Equal(t, []opensheet.Row{{"id": "1"}}, pages[0].Rows)
	require.False(t, pages[0].Stale)
	require.Equal(t, opensheet.ETag(`"e1"`), pages[0].ETag)
	require.Equal(t, []opensheet.Row{{"id": "2"}}, pages[1].Rows)
	require.True(t, pages[1].Stale)

	require.Equal(t, "limit=2&where=amount%3Anum.gte%3A100", page1Query)
	require.Equal(t, []string{"Bearer t", "Bearer t"}, authHeaders)
}

func TestCreateRow_IdempotencyKeyReplays(t *testing.T) {
	var keys []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/o1/projects/p1/sheets/s1", func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		writeJSON(t, w, http.StatusCreated, opensheet.Row{"id": "1", "name": "a"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	row1, _, err := c.CreateRow(t.Context(), "s1", opensheet.Row{"name": "a"}, opensheet.WithIdempotencyKey("k1"))
	require.NoError(t, err)
	row2, _, err := c.CreateRow(t.Context(), "s1", opensheet.Row{"name": "a"}, opensheet.WithIdempotencyKey("k1"))
	require.NoError(t, err)

	require.Equal(t, row1, row2)
	require.Equal(t, []string{"k1", "k1"}, keys)
}

func TestCreateRow_NumericColumns(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/o1/projects/p1/sheets/s1", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeJSON(t, w, http.StatusCreated, opensheet.Row{"id": "1", "amount": "12"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	_, _, err := c.CreateRow(t.Context(), "s1", opensheet.Row{"amount": "12"}, opensheet.WithNumericColumns("amount"))
	require.NoError(t, err)

	require.Equal(t, float64(12), body["amount"])
	require.Equal(t, []any{"amount"}, body["numeric_columns"])
}

func TestCreateRows_NumericColumns(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/o1/projects/p1/sheets/tx/rows/batch", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeJSON(t, w, http.StatusCreated, map[string]any{"ids": []string{"1", "2"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	rows := []opensheet.Row{
		{"amount": "12", "name": "a"},
		{"amount": "34", "name": "b"},
	}
	ids, err := c.CreateRows(t.Context(), "tx", rows, opensheet.WithNumericColumns("amount"))
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, ids)

	require.Equal(t, []any{"amount"}, body["numeric_columns"])
	gotRows, ok := body["rows"].([]any)
	require.True(t, ok, "rows must decode as an array, got %T", body["rows"])
	require.Len(t, gotRows, 2)
	row0, ok := gotRows[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(12), row0["amount"])
	require.Equal(t, "a", row0["name"])
	row1, ok := gotRows[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(34), row1["amount"])
	require.Equal(t, "b", row1["name"])
}

func TestCreateRows_RefusesOversizeBatchWithoutHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	rows := make([]opensheet.Row, 501)
	for i := range rows {
		rows[i] = opensheet.Row{"id": fmt.Sprintf("%d", i)}
	}

	_, err := c.CreateRows(t.Context(), "tx", rows)
	require.Error(t, err)
	require.True(t, opensheet.IsValidationError(err), "want ValidationError, got %v", err)
	require.False(t, called, "CreateRows must reject an oversize batch before making an HTTP call")
}

func TestPatchRow_IfMatchAndPreconditionFailed(t *testing.T) {
	var ifMatch string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/o1/projects/p1/sheets/s1/row1", func(w http.ResponseWriter, r *http.Request) {
		ifMatch = r.Header.Get("If-Match")
		writeErrorEnvelope(t, w, http.StatusPreconditionFailed, "SHT030", "stale etag")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)

	_, _, err := c.PatchRow(t.Context(), "s1", "row1", opensheet.Row{"name": "b"}, opensheet.WithIfMatch(`"v1"`))
	require.Equal(t, `"v1"`, ifMatch)
	require.Error(t, err)
	require.True(t, opensheet.IsPreconditionFailedError(err), "want PreconditionFailedError, got %v", err)
}

func TestErrorMapping(t *testing.T) {
	t.Run("404 maps to NotFoundError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorEnvelope(t, w, http.StatusNotFound, "SHT013", "not found")
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)

		_, _, err := c.Row(t.Context(), "s1", "row1")
		require.Error(t, err)
		require.True(t, opensheet.IsNotFoundError(err), "want NotFoundError, got %v", err)
	})

	t.Run("409 SHT036 maps to StaleCursorError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorEnvelope(t, w, http.StatusConflict, "SHT036", "stale cursor")
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)

		var lastErr error
		for _, err := range c.Rows(t.Context(), "s1", opensheet.Query{}) {
			lastErr = err
			break
		}
		require.Error(t, lastErr)
		require.True(t, opensheet.IsStaleCursorError(lastErr), "want StaleCursorError, got %v", lastErr)
	})

	t.Run("429 with Retry-After maps to RateLimitedError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			writeErrorEnvelope(t, w, http.StatusTooManyRequests, "SHT040", "slow down")
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)

		_, _, err := c.Row(t.Context(), "s1", "row1")
		require.Error(t, err)
		require.True(t, opensheet.IsRateLimitedError(err), "want RateLimitedError, got %v", err)
		var rle *opensheet.RateLimitedError
		require.ErrorAs(t, err, &rle)
		require.Equal(t, 2*time.Second, rle.RetryAfter)
	})

	t.Run("413 maps to PayloadTooLargeError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorEnvelope(t, w, http.StatusRequestEntityTooLarge, "SHT041", "too big")
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)

		_, _, err := c.CreateRow(t.Context(), "s1", opensheet.Row{"a": "1"})
		require.Error(t, err)
		require.True(t, opensheet.IsPayloadTooLargeError(err), "want PayloadTooLargeError, got %v", err)
	})

	t.Run("422 SHT028 maps to ValidationError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorEnvelope(t, w, http.StatusUnprocessableEntity, "SHT028", "invalid")
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)

		_, _, err := c.CreateRow(t.Context(), "s1", opensheet.Row{"a": "1"})
		require.Error(t, err)
		require.True(t, opensheet.IsValidationError(err), "want ValidationError, got %v", err)
	})
}

func TestDeleteRow_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	err := c.DeleteRow(t.Context(), "s1", "row1")
	require.NoError(t, err)
}

func TestNew_PrivateHostGuard(t *testing.T) {
	_, err := opensheet.New(opensheet.Config{
		BaseURL: "http://127.0.0.1:9",
		Org:     "o1",
		Project: "p1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "private host")

	c, err := opensheet.New(opensheet.Config{
		BaseURL:           "http://127.0.0.1:9",
		Org:               "o1",
		Project:           "p1",
		AllowPrivateHosts: true,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}
