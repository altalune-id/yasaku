package boot_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func dbDigest(t *testing.T, conn *sql.DB) map[string][32]byte {
	t.Helper()
	// NOTE: sessions change on every request by design; every other table must stay byte-identical across a support GET.
	ignored := map[string]bool{"yasaku_sessions": true}
	ctx := context.Background()
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		if !ignored[name] {
			tables = append(tables, name)
		}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	out := make(map[string][32]byte, len(tables))
	for _, table := range tables {
		rs, err := conn.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY 1")
		require.NoError(t, err)
		cols, err := rs.Columns()
		require.NoError(t, err)
		h := sha256.New()
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			require.NoError(t, rs.Scan(ptrs...))
			_, _ = fmt.Fprintf(h, "%v\n", vals)
		}
		require.NoError(t, rs.Err())
		require.NoError(t, rs.Close())
		out[table] = [32]byte(h.Sum(nil))
	}
	return out
}

func changedTables(before, after map[string][32]byte) []string {
	var out []string
	for name, sum := range after {
		if before[name] != sum {
			out = append(out, name)
		}
	}
	return out
}

func supportServer(t *testing.T) (*boot.Server, session.Principal) {
	t.Helper()
	srv, _ := newScopeProbeServer(t, config.ModeCloud)
	ctx := context.Background()
	staff, err := srv.Users.Create(ctx, user.CreateRequest{Email: "probe-staff@example.com", Name: "Staff", Source: user.SourceOIDC})
	require.NoError(t, err)
	_, err = srv.Orgs.BootstrapSingleton(ctx, "ops", "Ops", staff.ID)
	require.NoError(t, err)
	owner, err := srv.Users.Create(ctx, user.CreateRequest{Email: "probe-owner@example.com", Name: "Owner", Source: user.SourceOIDC})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "probe-org", Name: "Probe Org", OwnerID: owner.ID})
	require.NoError(t, err)
	_, err = srv.Projects.Create(fakes.Owner(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID}), o.ID, "probe-project", "Probe Project")
	require.NoError(t, err)
	p := session.Principal{UserID: staff.ID, Email: staff.Email, Source: session.SourceOIDC,
		Support: &session.Support{OrgID: o.ID, Reason: "ticket 42", Until: time.Now().Add(time.Hour), EventID: uuid.New()}}
	return srv, p
}

// TestSupportSession_EveryGETRendersReadOnly walks every console GET inside the visited org as staff on a live support visit. SECURITY: a GET that writes lazily would let a read-only visit change the org.
func TestSupportSession_EveryGETRendersReadOnly(t *testing.T) {
	srv, p := supportServer(t)
	conn := srv.Platform.Pool.W
	cookie := probeCookie(t, srv, p)
	walked := 0
	for _, rt := range probeRoutes() {
		if rt.method != http.MethodGet || !strings.HasPrefix(rt.path, "/orgs/probe-org") {
			continue
		}
		walked++
		t.Run(rt.path, func(t *testing.T) {
			before := dbDigest(t, conn)
			req := httptest.NewRequest(http.MethodGet, rt.path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			srv.Web.ServeHTTP(rec, req)

			require.Less(t, rec.Code, 500, "body=%s", rec.Body.String())
			require.NotEqual(t, "/admin", rec.Header().Get("Location"), "a live visit is not bounced")
			require.Empty(t, changedTables(before, dbDigest(t, conn)), "a support GET wrote to the database")
			if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<html") {
				require.Contains(t, rec.Body.String(), "data-support-banner", "every full page shows the support banner")
			}
		})
	}
	require.Greater(t, walked, 20, "the walk covers the org and project pages")
}
