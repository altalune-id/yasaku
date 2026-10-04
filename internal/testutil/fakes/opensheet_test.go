package fakes_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/opensheet"
)

func fakeClient(t *testing.T, f *fakes.Opensheet, token string) *opensheet.Client {
	t.Helper()
	c, err := opensheet.New(opensheet.Config{
		BaseURL: f.URL(), Org: "acme", Project: "home", Token: token,
		AllowPrivateHosts: true, Retry: httpclient.RetryPolicy{MaxAttempts: 1},
	})
	require.NoError(t, err)
	return c
}

func TestOpensheet_RowLifecycle(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "osk_good")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "osk_good")
	ctx := t.Context()

	caps, err := c.Capabilities(ctx, "tx")
	require.NoError(t, err)
	require.False(t, caps.IDColumn, "an empty soft delete tab reports only deleted_at, so no id column")
	require.True(t, caps.SatisfiesContract)
	require.True(t, caps.SoftDelete)
	require.True(t, caps.Writable)

	_, _, err = c.PatchRow(ctx, "tx", "r1", opensheet.Row{"name": "a"})
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok)
	require.True(t, nf.RowMissing())

	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"}, opensheet.WithIdempotencyKey("r1:1"))
	require.NoError(t, err)
	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"}, opensheet.WithIdempotencyKey("r1:1"))
	require.NoError(t, err, "same key, same body replays")
	require.Len(t, f.Rows("tx"), 1)

	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "z"}, opensheet.WithIdempotencyKey("r1:1"))
	ve, ok := errors.AsType[*opensheet.ValidationError](err)
	require.True(t, ok)
	require.Equal(t, "SHT018", ve.Code)

	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "b"})
	ae, ok := errors.AsType[*opensheet.APIError](err)
	require.True(t, ok)
	require.Equal(t, http.StatusConflict, ae.Status)
	require.Equal(t, "SHT012", ae.Code)

	_, _, err = c.PatchRow(ctx, "tx", "r1", opensheet.Row{"name": "b"})
	require.NoError(t, err)
	row, _ := f.Row("tx", "r1")
	require.Equal(t, "b", row["name"])

	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"))
	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"), "a tombstoned row deletes again as a no-op")
	row, _ = f.Row("tx", "r1")
	require.NotEmpty(t, row["deleted_at"])
}

func TestOpensheet_MasksABadKeyAsA404(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "osk_good")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id"}, Writable: true})
	_, err := fakeClient(t, f, "osk_bad").Capabilities(t.Context(), "tx")
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok)
	require.Equal(t, "SHT001", nf.Code)
}

func TestOpensheet_RefusesWritesOnAReadOnlySheetAndADeleteWithoutDeletedAt(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("ro", fakes.OpensheetSheet{Columns: []string{"id", "deleted_at"}})
	f.AddSheet("nodel", fakes.OpensheetSheet{Columns: []string{"id"}, Writable: true})
	c := fakeClient(t, f, "k")
	_, _, err := c.CreateRow(t.Context(), "ro", opensheet.Row{"id": "a"})
	ae, ok := errors.AsType[*opensheet.APIError](err)
	require.True(t, ok)
	require.Equal(t, http.StatusForbidden, ae.Status)

	_, _, err = c.CreateRow(t.Context(), "nodel", opensheet.Row{"id": "a"})
	require.NoError(t, err)
	err = c.DeleteRow(t.Context(), "nodel", "a")
	ve, ok := errors.AsType[*opensheet.ValidationError](err)
	require.True(t, ok)
	require.Equal(t, "SHT029", ve.Code)
}

func TestOpensheet_FailNextInjectsAStatusThenRecovers(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "deleted_at"}, Writable: true})
	f.FailNext(http.MethodPost, "tx", http.StatusTooManyRequests, "GSH429", 1)
	c := fakeClient(t, f, "k")
	_, _, err := c.CreateRow(t.Context(), "tx", opensheet.Row{"id": "a"})
	require.True(t, opensheet.IsRateLimitedError(err))
	_, _, err = c.CreateRow(t.Context(), "tx", opensheet.Row{"id": "a"})
	require.NoError(t, err)
	require.Equal(t, 2, f.CountRequests(http.MethodPost, "tx"))
}

func sendOpensheet(t *testing.T, f *fakes.Opensheet, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.URL()+"/api/v1/orgs/acme/projects/home/sheets/"+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	if resp.StatusCode != http.StatusNoContent {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

func errorCodeOf(t *testing.T, out map[string]any) string {
	t.Helper()
	env, ok := out["error"].(map[string]any)
	require.True(t, ok, "want an error envelope, got %v", out)
	code, _ := env["code"].(string)
	return code
}

func TestOpensheet_PatchOnATombstonedRowKeepsTheTombstone(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "k")
	ctx := t.Context()
	_, _, err := c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"})
	require.NoError(t, err)
	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"))
	before, _ := f.Row("tx", "r1")

	got, _, err := c.PatchRow(ctx, "tx", "r1", opensheet.Row{"name": "b"})
	require.NoError(t, err)
	require.Equal(t, opensheet.Row{"id": "r1", "name": "b"}, got)
	row, _ := f.Row("tx", "r1")
	require.Equal(t, "b", row["name"])
	require.Equal(t, before["deleted_at"], row["deleted_at"])
	require.NotEmpty(t, row["deleted_at"])

	_, _, err = c.Row(ctx, "tx", "r1")
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok)
	require.True(t, nf.RowMissing(), "GET still hides a tombstone")
}

func TestOpensheet_ReplaceRowNeedsALiveRowAndClearsOmittedColumns(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "note", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "k")
	ctx := t.Context()

	_, _, err := c.ReplaceRow(ctx, "tx", "r1", opensheet.Row{"name": "a"})
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok, "want NotFoundError, got %v", err)
	require.True(t, nf.RowMissing())

	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a", "note": "n"})
	require.NoError(t, err)
	got, _, err := c.ReplaceRow(ctx, "tx", "r1", opensheet.Row{"name": "b"})
	require.NoError(t, err)
	require.Equal(t, opensheet.Row{"id": "r1", "name": "b", "note": ""}, got)
	row, _ := f.Row("tx", "r1")
	require.Equal(t, map[string]string{"id": "r1", "name": "b", "note": "", "deleted_at": ""}, row)

	_, _, err = c.ReplaceRow(ctx, "tx", "r1", opensheet.Row{"id": "r1", "name": "c"})
	require.NoError(t, err, "a body id equal to the path is accepted")

	status, out := sendOpensheet(t, f, http.MethodPut, "tx/rows/r1", "k", `{"id":"r2","name":"c"}`)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "SHT027", errorCodeOf(t, out))

	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"))
	_, _, err = c.ReplaceRow(ctx, "tx", "r1", opensheet.Row{"name": "d"})
	nf, ok = errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok, "want NotFoundError, got %v", err)
	require.True(t, nf.RowMissing(), "a tombstone is not live")
}

func TestOpensheet_RefusesReadOnlyColumnsAndEmptyPatches(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "k")
	ctx := t.Context()
	_, _, err := c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"})
	require.NoError(t, err)

	for _, tc := range []struct {
		name, method, path, body, code string
	}{
		{"deleted_at on create", http.MethodPost, "tx/rows", `{"id":"r2","deleted_at":"x"}`, "SHT015"},
		{"deleted_at on patch", http.MethodPatch, "tx/rows/r1", `{"deleted_at":"x"}`, "SHT015"},
		{"deleted_at on replace", http.MethodPut, "tx/rows/r1", `{"deleted_at":"x"}`, "SHT015"},
		{"same id on patch", http.MethodPatch, "tx/rows/r1", `{"id":"r1"}`, "SHT015"},
		{"other id on patch", http.MethodPatch, "tx/rows/r1", `{"id":"r9"}`, "SHT015"},
		{"empty patch", http.MethodPatch, "tx/rows/r1", `{}`, "SHT016"},
		{"blank id on create", http.MethodPost, "tx/rows", `{"id":" "}`, "SHT016"},
		{"unknown column", http.MethodPatch, "tx/rows/r1", `{"nope":"x"}`, "SHT014"},
		{"undecodable body", http.MethodPatch, "tx/rows/r1", `not json`, "GEN004"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, out := sendOpensheet(t, f, tc.method, tc.path, "k", tc.body)
			require.Equal(t, http.StatusBadRequest, status)
			require.Equal(t, tc.code, errorCodeOf(t, out))
		})
	}
	row, _ := f.Row("tx", "r1")
	require.Equal(t, map[string]string{"id": "r1", "name": "a", "deleted_at": ""}, row)
}

func TestOpensheet_AnswersTheUnprocessableCodesWith422(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	f.AddSheet("nodel", fakes.OpensheetSheet{Columns: []string{"id"}, Writable: true})
	c := fakeClient(t, f, "k")
	_, _, err := c.CreateRow(t.Context(), "tx", opensheet.Row{"id": "r1", "name": "a"}, opensheet.WithIdempotencyKey("k1"))
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		f.URL()+"/api/v1/orgs/acme/projects/home/sheets/tx/rows", strings.NewReader(`{"id":"r1","name":"z"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer k")
	req.Header.Set("Idempotency-Key", "k1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, "SHT018")

	status, out := sendOpensheet(t, f, http.MethodPut, "tx/rows/r1", "k", `{"id":"r2"}`)
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "SHT027", errorCodeOf(t, out))

	status, out = sendOpensheet(t, f, http.MethodDelete, "nodel/rows/a", "k", "")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "SHT029", errorCodeOf(t, out))
}

func TestOpensheet_LogsMaskedAndUnroutedRequests(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})

	status, _ := sendOpensheet(t, f, http.MethodPost, "tx/rows", "bad", `{"id":"r1"}`)
	require.Equal(t, http.StatusNotFound, status)
	status, out := sendOpensheet(t, f, http.MethodGet, "tx/nowhere/at/all", "k", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "SHT001", errorCodeOf(t, out))

	reqs := f.Requests()
	require.Len(t, reqs, 2)
	require.Equal(t, fakes.OpensheetRequest{Method: http.MethodPost, Slug: "tx", Body: map[string]string{"id": "r1"}}, reqs[0])
	require.Equal(t, http.MethodGet, reqs[1].Method)
	require.Equal(t, 1, f.CountRequests(http.MethodPost, "tx"))
}

func TestOpensheet_LogsTheIdempotencyKey(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	_, _, err := fakeClient(t, f, "k").CreateRow(t.Context(), "tx", opensheet.Row{"id": "r1", "name": "a"}, opensheet.WithIdempotencyKey("r1:1"))
	require.NoError(t, err)
	require.Equal(t, []fakes.OpensheetRequest{{
		Method: http.MethodPost, Slug: "tx", IdempotencyKey: "r1:1",
		Body: map[string]string{"id": "r1", "name": "a"},
	}}, f.Requests())
}

func TestOpensheet_HidesTombstonesFromResponsesAndRowCount(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "k")
	ctx := t.Context()

	created, _, err := c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"})
	require.NoError(t, err)
	require.Equal(t, opensheet.Row{"id": "r1", "name": "a"}, created)
	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r2", "name": "b"})
	require.NoError(t, err)
	read, _, err := c.Row(ctx, "tx", "r2")
	require.NoError(t, err)
	require.Equal(t, opensheet.Row{"id": "r2", "name": "b"}, read)
	patched, _, err := c.PatchRow(ctx, "tx", "r2", opensheet.Row{"name": "c"})
	require.NoError(t, err)
	require.Equal(t, opensheet.Row{"id": "r2", "name": "c"}, patched)

	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"))
	status, out := sendOpensheet(t, f, http.MethodGet, "tx/capabilities", "k", "")
	require.Equal(t, http.StatusOK, status)
	require.InDelta(t, 1, out["rowCount"], 0)
}

func TestOpensheet_SetTokenRotatesTheKey(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "osk_old")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id"}, Writable: true})
	_, err := fakeClient(t, f, "osk_old").Capabilities(t.Context(), "tx")
	require.NoError(t, err)

	f.SetToken("osk_new")
	_, err = fakeClient(t, f, "osk_old").Capabilities(t.Context(), "tx")
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	require.True(t, ok, "want NotFoundError, got %v", err)
	require.Equal(t, "SHT001", nf.Code)
	_, err = fakeClient(t, f, "osk_new").Capabilities(t.Context(), "tx")
	require.NoError(t, err)
}

func TestOpensheet_IsSafeForConcurrentUse(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "deleted_at"}, Writable: true})
	c := fakeClient(t, f, "k")
	var g errgroup.Group
	for i := range 16 {
		g.Go(func() error {
			if _, _, err := c.CreateRow(t.Context(), "tx", opensheet.Row{"id": fmt.Sprintf("r%d", i)}); err != nil {
				return err
			}
			_ = f.CountRequests(http.MethodPost, "tx")
			_ = f.Rows("tx")
			return nil
		})
	}
	require.NoError(t, g.Wait())
	require.Len(t, f.Rows("tx"), 16)
	require.Equal(t, 16, f.CountRequests(http.MethodPost, "tx"))
}

func TestOpensheet_ReportsColumnsFromTheFirstLiveRowLikeTheRealServer(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"name", "id", "note", "deleted_at"}, Writable: true})
	f.AddSheet("plain", fakes.OpensheetSheet{Columns: []string{"id", "name"}, Writable: true})
	c := fakeClient(t, f, "k")
	ctx := t.Context()

	caps, err := c.Capabilities(ctx, "tx")
	require.NoError(t, err)
	require.Equal(t, []string{"deleted_at"}, caps.Columns, "an empty tab shows only the soft delete flag")
	require.False(t, caps.IDColumn, "opensheet judges id from [deleted_at] and finds none")
	require.True(t, caps.SatisfiesContract)
	require.NotNil(t, caps.ValidatedAt, "publish validated the tab")
	require.Zero(t, caps.RowCount)
	caps, err = c.Capabilities(ctx, "plain")
	require.NoError(t, err)
	require.Empty(t, caps.Columns, "an empty tab without deleted_at shows no column at all")
	require.True(t, caps.IDColumn, "no columns falls back to publish's verdict")

	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r1", "name": "a"})
	require.NoError(t, err)
	_, _, err = c.CreateRow(ctx, "tx", opensheet.Row{"id": "r2"})
	require.NoError(t, err)
	caps, err = c.Capabilities(ctx, "tx")
	require.NoError(t, err)
	require.Equal(t, []string{"deleted_at", "id", "name", "note"}, caps.Columns, "every header key of the first live row, empty cells included, sorted")

	require.NoError(t, c.DeleteRow(ctx, "tx", "r1"))
	require.NoError(t, c.DeleteRow(ctx, "tx", "r2"))
	caps, err = c.Capabilities(ctx, "tx")
	require.NoError(t, err)
	require.Equal(t, []string{"deleted_at"}, caps.Columns, "tombstones do not count as the first live row")
}

func TestOpensheet_SeedRowFillsEveryHeaderKey(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"id", "name", "deleted_at"}, Writable: true})
	f.SeedRow("tx", map[string]string{"id": "r1"})
	row, ok := f.Row("tx", "r1")
	require.True(t, ok)
	require.Equal(t, map[string]string{"id": "r1", "name": "", "deleted_at": ""}, row)
	caps, err := fakeClient(t, f, "k").Capabilities(t.Context(), "tx")
	require.NoError(t, err)
	require.Equal(t, []string{"deleted_at", "id", "name"}, caps.Columns)
	require.EqualValues(t, 1, caps.RowCount)
}

func TestOpensheet_HeaderColumnsEmulatesTheFixedServer(t *testing.T) {
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.SetHeaderColumns(true)
	f.AddSheet("tx", fakes.OpensheetSheet{Columns: []string{"name", "id", "deleted_at"}, Writable: true})
	caps, err := fakeClient(t, f, "k").Capabilities(t.Context(), "tx")
	require.NoError(t, err)
	require.Equal(t, []string{"deleted_at", "id", "name"}, caps.Columns, "the header row, sorted, even with no row")
	require.Zero(t, caps.RowCount)
}
