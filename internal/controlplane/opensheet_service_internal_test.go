package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/opensheetsync"
)

func opensheetProcedures(ps []string) []string {
	var out []string
	for _, p := range ps {
		if strings.HasPrefix(p, "/yasaku.v1.OpensheetService/") {
			out = append(out, p)
		}
	}
	return out
}

func serveGetOpensheetLink(h http.Handler) int {
	req := httptest.NewRequest(http.MethodPost, "/api"+yasakuv1connect.OpensheetServiceGetOpensheetLinkProcedure, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestOpensheetService_MountedOnlyWithTheModule(t *testing.T) {
	off := New(nil, nil, Deps{})
	require.Nil(t, off.OpensheetSvc)
	require.Equal(t, http.StatusNotFound, serveGetOpensheetLink(off.Handler("")), "no opensheet.baseURL, no route")
	require.Empty(t, opensheetProcedures(off.MountedProcedures()), "no opensheet.baseURL, no procedures")

	on := New(nil, nil, Deps{Opensheet: &opensheetsync.Service{}})
	require.NotEqual(t, http.StatusNotFound, serveGetOpensheetLink(on.Handler("")), "the module mounts the route")
	got := opensheetProcedures(on.MountedProcedures())
	require.ElementsMatch(t, []string{
		yasakuv1connect.OpensheetServiceGetOpensheetLinkProcedure,
		yasakuv1connect.OpensheetServiceTestOpensheetLinkProcedure,
		yasakuv1connect.OpensheetServiceSaveOpensheetLinkProcedure,
		yasakuv1connect.OpensheetServiceSetOpensheetLinkEnabledProcedure,
		yasakuv1connect.OpensheetServiceSyncOpensheetNowProcedure,
		yasakuv1connect.OpensheetServiceDeleteOpensheetLinkProcedure,
	}, got)
	table := ScopeTable()
	verbs := VerbTable()
	for _, p := range got {
		require.NotEmpty(t, table[p], p)
		require.Equal(t, "opensheetsync", verbs[p].Module, p)
	}
}

func TestOpensheetLink_ToProtoNeverCarriesTheKey(t *testing.T) {
	require.Nil(t, toProtoOpensheetLink(nil, opensheetsync.Backlog{}))
	at := time.Date(2026, 10, 3, 1, 2, 3, 0, time.FixedZone("WIB", 7*3600))
	l := opensheetsync.NewLink(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, at)
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed-bytes"), "abcd", at)
	p := toProtoOpensheetLink(l, opensheetsync.Backlog{Pending: 7, Failing: 2, GivenUp: 1})
	require.Equal(t, "abcd", p.GetApiKeyHint())
	require.Equal(t, "2026-10-02T18:02:03Z", p.GetVerifiedAt(), "instants go out in UTC")
	require.Empty(t, p.GetLastSyncedAt())
	require.EqualValues(t, 7, p.GetPending())
	require.EqualValues(t, 2, p.GetFailing())
	require.EqualValues(t, 1, p.GetGivenUp())
	require.True(t, p.GetAwaitingFirstSync(), "no sync has landed since the Save")
	require.NotContains(t, p.String(), "sealed-bytes")

	fields := p.ProtoReflect().Descriptor().Fields()
	for i := range fields.Len() {
		name := string(fields.Get(i).Name())
		require.False(t, strings.Contains(name, "sealed") || name == "api_key", "OpensheetLink.%s could carry the key (Revision 5)", name)
	}

	synced := at.Add(time.Minute)
	l.LastSyncedAt = &synced
	p = toProtoOpensheetLink(l, opensheetsync.Backlog{})
	require.False(t, p.GetAwaitingFirstSync(), "a sync landed after the Save")
	require.Equal(t, "2026-10-02T18:03:03Z", p.GetLastSyncedAt(), "a zoned instant still goes out in UTC")
}

func TestOpensheetChecks_CarryTheOSLCode(t *testing.T) {
	cl := opensheetsync.Checklist{
		{Entity: opensheetsync.EntityWallet, Sheet: "w", Reachable: true, ColumnsDeferred: true},
		{Entity: opensheetsync.EntityCategory, Sheet: "c", Reachable: true, Missing: []string{"color"},
			Err: &opensheetsync.ShapeMismatchError{Sheet: "c", Missing: []string{"color"}}},
		{Entity: opensheetsync.EntityTransaction, Sheet: "t", Reachable: true, IDColumn: true, Writable: false,
			Err: &opensheetsync.SheetNotWritableError{Sheet: "t"}},
	}
	got := toProtoTabChecks(cl)
	require.Len(t, got, 3)
	require.True(t, got[0].GetOk())
	require.Empty(t, got[0].GetCode())
	require.True(t, got[0].GetColumnsDeferred())
	require.Equal(t, "wallet", got[0].GetEntity())
	require.False(t, got[1].GetColumnsDeferred())
	require.False(t, got[1].GetOk())
	require.Equal(t, "OSL005", got[1].GetCode())
	require.Equal(t, []string{"color"}, got[1].GetMissing())
	require.True(t, got[2].GetIdColumn())
	require.False(t, got[2].GetWritable())
	require.Equal(t, "OSL007", got[2].GetCode())
}
