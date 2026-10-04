package opensheetsync_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/opensheet"
)

func walletTab(t *testing.T) opensheetsync.Tab {
	t.Helper()
	tab, ok := opensheetsync.TabFor(opensheetsync.EntityWallet)
	require.True(t, ok)
	return tab
}

func TestCheckTab(t *testing.T) {
	full := walletTab(t).Columns
	validated := &time.Time{}
	noDeletedAt := slices.DeleteFunc(slices.Clone(full), func(c string) bool { return c == "deleted_at" })
	tests := []struct {
		name     string
		caps     opensheet.SheetCapabilities
		err      error
		is       func(error) bool
		missing  []string
		deferred bool
	}{
		{"passes", opensheet.SheetCapabilities{Columns: append([]string{"extra"}, full...), IDColumn: true, SoftDelete: true, Writable: true, SatisfiesContract: true, RowCount: 3}, nil, nil, nil, false},
		{"404 mask", opensheet.SheetCapabilities{}, &opensheet.NotFoundError{Code: "SHT001"}, opensheetsync.IsSheetUnreachableError, nil, false},
		{"401 bad key", opensheet.SheetCapabilities{}, &opensheet.APIError{Status: 401, Code: "AUT001"}, opensheetsync.IsSheetUnreachableError, nil, false},
		{"403 key without the scope", opensheet.SheetCapabilities{}, &opensheet.APIError{Status: 403, Code: "AUT003"}, opensheetsync.IsSheetUnreachableError, nil, false},
		{"validation", opensheet.SheetCapabilities{}, &opensheet.ValidationError{Code: "SHT020", Message: "bad slug"}, opensheetsync.IsContractUnsatisfiedError, nil, false},
		{"opensheet down", opensheet.SheetCapabilities{}, &opensheet.APIError{Status: 502}, opensheetsync.IsUnavailableError, nil, false},
		{"rate limited", opensheet.SheetCapabilities{}, &opensheet.RateLimitedError{}, opensheetsync.IsUnavailableError, nil, false},
		{"network", opensheet.SheetCapabilities{}, errors.New("dial tcp: connection refused"), opensheetsync.IsUnavailableError, nil, false},
		{"missing columns", opensheet.SheetCapabilities{Columns: []string{"id", "name"}, IDColumn: true, Writable: true, SatisfiesContract: true, RowCount: 1},
			nil, opensheetsync.IsShapeMismatchError, []string{"kind", "provider", "currency", "balance", "exclude_from_total", "archived", "updated_at", "deleted_at"}, false},
		{"a populated tab missing one column is still refused", opensheet.SheetCapabilities{Columns: slices.DeleteFunc(slices.Clone(full), func(c string) bool { return c == "provider" }), IDColumn: true, SoftDelete: true, Writable: true, SatisfiesContract: true, RowCount: 4},
			nil, opensheetsync.IsShapeMismatchError, []string{"provider"}, false},
		{"header columns on an empty tab are still checked", opensheet.SheetCapabilities{Columns: noDeletedAt, IDColumn: true, Writable: true, SatisfiesContract: true},
			nil, opensheetsync.IsShapeMismatchError, []string{"deleted_at"}, false},
		{"empty tab, no columns", opensheet.SheetCapabilities{ValidatedAt: validated, IDColumn: true, SoftDelete: true, Writable: true, SatisfiesContract: true}, nil, nil, nil, true},
		{"empty tab, only deleted_at", opensheet.SheetCapabilities{ValidatedAt: validated, Columns: []string{"deleted_at"}, SoftDelete: true, Writable: true, SatisfiesContract: true}, nil, nil, nil, true},
		{"empty tab without soft delete", opensheet.SheetCapabilities{ValidatedAt: validated, IDColumn: true, Writable: true, SatisfiesContract: true}, nil, opensheetsync.IsShapeMismatchError, []string{"deleted_at"}, true},
		{"empty tab, read only", opensheet.SheetCapabilities{ValidatedAt: validated, Columns: []string{"deleted_at"}, SoftDelete: true, SatisfiesContract: true}, nil, opensheetsync.IsSheetNotWritableError, nil, true},
		{"empty tab, contract fails", opensheet.SheetCapabilities{ValidatedAt: validated, Columns: []string{"deleted_at"}, SoftDelete: true, Writable: true, ContractReason: "no id column"}, nil, opensheetsync.IsContractUnsatisfiedError, nil, true},
		{"empty tab never validated by opensheet", opensheet.SheetCapabilities{Columns: []string{"deleted_at"}, SoftDelete: true, Writable: true, SatisfiesContract: true}, nil, opensheetsync.IsContractUnsatisfiedError, nil, true},
		{"no id column", opensheet.SheetCapabilities{Columns: full, SoftDelete: true, Writable: true, SatisfiesContract: true, RowCount: 1}, nil, opensheetsync.IsNoIDColumnError, nil, false},
		{"read only", opensheet.SheetCapabilities{Columns: full, IDColumn: true, SoftDelete: true, SatisfiesContract: true, RowCount: 1}, nil, opensheetsync.IsSheetNotWritableError, nil, false},
		{"contract", opensheet.SheetCapabilities{Columns: full, IDColumn: true, SoftDelete: true, Writable: true, ContractReason: "duplicate ids", RowCount: 1}, nil, opensheetsync.IsContractUnsatisfiedError, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := opensheetsync.CheckTab(walletTab(t), "yasaku-wallets", tt.caps, tt.err)
			require.Equal(t, "yasaku-wallets", c.Sheet)
			require.Equal(t, tt.missing, c.Missing)
			require.Equal(t, tt.deferred, c.ColumnsDeferred)
			if tt.is == nil {
				require.True(t, c.OK(), "%v", c.Err)
				return
			}
			require.False(t, c.OK())
			require.True(t, tt.is(c.Err), "got %v", c.Err)
		})
	}
}

func TestCheckTab_ADeferredTabTakesTheIDColumnFromTheContractVerdict(t *testing.T) {
	caps := opensheet.SheetCapabilities{Columns: []string{"deleted_at"}, SoftDelete: true, Writable: true, SatisfiesContract: true, ValidatedAt: &time.Time{}}
	c := opensheetsync.CheckTab(walletTab(t), "yasaku-wallets", caps, nil)
	require.True(t, c.ColumnsDeferred)
	require.True(t, c.IDColumn, "opensheet judged id from [deleted_at]; publish's contract verdict already required it")
	caps.SatisfiesContract, caps.ContractReason = false, "no id column"
	c = opensheetsync.CheckTab(walletTab(t), "yasaku-wallets", caps, nil)
	require.False(t, c.IDColumn)
	require.True(t, opensheetsync.IsContractUnsatisfiedError(c.Err), "opensheet's reason names the cause")

	caps.SatisfiesContract, caps.ContractReason, caps.ValidatedAt = true, "", nil
	c = opensheetsync.CheckTab(walletTab(t), "yasaku-wallets", caps, nil)
	require.False(t, c.IDColumn, "without a validation the contract verdict proves nothing, as in opensheet's idColumnKnown")
	require.True(t, opensheetsync.IsContractUnsatisfiedError(c.Err))
	require.Contains(t, c.ContractReason, "not read and validated")
}

func TestChecklist_ErrIsTheFirstFailingTabAndAnAppError(t *testing.T) {
	ok := opensheetsync.TabCheck{Sheet: "a"}
	bad1 := opensheetsync.TabCheck{Sheet: "b", Err: &opensheetsync.NoIDColumnError{Sheet: "b"}}
	bad2 := opensheetsync.TabCheck{Sheet: "c", Err: &opensheetsync.SheetNotWritableError{Sheet: "c"}}
	l := opensheetsync.Checklist{ok, bad1, bad2}
	require.False(t, l.OK())
	require.True(t, opensheetsync.IsNoIDColumnError(l.Err()))
	ae, isApp := apperror.AsAppError(l.Err())
	require.True(t, isApp, "the RPC and the console map it to a code")
	require.Equal(t, apperror.CodeOpensheetNoIDColumn, ae.Code())
	require.NoError(t, opensheetsync.Checklist{ok}.Err())
	require.False(t, opensheetsync.Checklist{}.OK(), "an empty checklist never passes")
}

func TestCheckTab_AgainstTheFake_HeaderModeChecksAnEmptyTabStrictly(t *testing.T) {
	tab := walletTab(t)
	short := slices.DeleteFunc(slices.Clone(tab.Columns), func(c string) bool { return c == "provider" })
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})
	c, err := opensheet.New(opensheet.Config{BaseURL: f.URL(), Org: "acme", Project: "home", Token: "k",
		AllowPrivateHosts: true, Retry: httpclient.RetryPolicy{MaxAttempts: 1}})
	require.NoError(t, err)

	caps, err := c.Capabilities(t.Context(), tab.DefaultSlug)
	got := opensheetsync.CheckTab(tab, tab.DefaultSlug, caps, err)
	require.True(t, got.OK(), "first-live-row mode: an empty tab cannot show the gap, %v", got.Err)
	require.True(t, got.ColumnsDeferred)

	f.SetHeaderColumns(true)
	caps, err = c.Capabilities(t.Context(), tab.DefaultSlug)
	got = opensheetsync.CheckTab(tab, tab.DefaultSlug, caps, err)
	require.True(t, opensheetsync.IsShapeMismatchError(got.Err), "%v", got.Err)
	require.Equal(t, []string{"provider"}, got.Missing)
	require.False(t, got.ColumnsDeferred)
}

// NOTE: "localhost" is a host name, so opensheet.New lets it through and only the dial guard refuses it, as with *.railway.internal.
func localhostURL(rawURL string) string { return strings.Replace(rawURL, "127.0.0.1", "localhost", 1) }

func TestCheckTab_APrivateHostNameIsAConfigFaultNotUnavailable(t *testing.T) {
	tab := walletTab(t)
	f := fakes.NewOpensheet(t, "acme", "home", "k")
	f.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: true})
	base := localhostURL(f.URL())
	c, err := opensheet.New(opensheet.Config{BaseURL: base, Org: "acme", Project: "home", Token: "k",
		Retry: httpclient.RetryPolicy{MaxAttempts: 1}})
	require.NoError(t, err, "a host name passes the client's literal-IP check")

	caps, err := c.Capabilities(t.Context(), tab.DefaultSlug)
	require.True(t, httpclient.IsPrivateAddressError(err), "%v", err)
	got := opensheetsync.CheckTab(tab, tab.DefaultSlug, caps, err)
	require.True(t, opensheetsync.IsPrivateEndpointError(got.Err), "%v", got.Err)
	require.False(t, opensheetsync.IsUnavailableError(got.Err), "retrying never helps")
	ae, ok := apperror.AsAppError(got.Err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeOpensheetPrivateEndpoint, ae.Code())
	require.Contains(t, ae.Message(), "YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true")
	require.NotContains(t, ae.Message(), strings.TrimPrefix(base, "http://"))
	require.Zero(t, f.CountRequests("GET", tab.DefaultSlug), "the dial guard refused before any request")
}
