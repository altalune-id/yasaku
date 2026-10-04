package opensheetsync_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/tenant"
)

// NOTE: storeFixture is one Store under test plus the seeders standing in for the wallets, categories and transactions tables.
type storeFixture struct {
	store       opensheetsync.Store
	tc          tenant.Context
	other       tenant.Context
	sibling     uuid.UUID
	wallet      func(t *testing.T, tc tenant.Context) uuid.UUID
	category    func(t *testing.T, tc tenant.Context) uuid.UUID
	transaction func(t *testing.T, tc tenant.Context, walletID, toWalletID, categoryID uuid.UUID) uuid.UUID
	remove      func(t *testing.T, tc tenant.Context, e opensheetsync.Entity, id uuid.UUID)
}

func (f storeFixture) ctx(tc tenant.Context) context.Context {
	return tenant.Into(context.Background(), tc)
}

func linkFor(tc tenant.Context, at time.Time) *opensheetsync.Link {
	l := opensheetsync.NewLink(tc.OrgID, tc.ProjectID, tc.UserID, uuid.Nil, at)
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, []byte("sealed"), "abcd", at)
	return l
}

func ref(e opensheetsync.Entity, id uuid.UUID) opensheetsync.Ref {
	return opensheetsync.Ref{Entity: e, ID: id}
}

// NOTE: runStoreContract pins the semantics both real stores and fakes.OpensheetSync share.
func runStoreContract(t *testing.T, newFixture func(t *testing.T) storeFixture) {
	at := time.Date(2026, 10, 3, 1, 2, 3, 456789000, time.UTC)

	t.Run("link round trip and upsert by project", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		_, err := f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.True(t, opensheetsync.IsLinkNotFoundError(err))
		on, err := f.store.LinkEnabled(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.False(t, on, "no link reads as off")

		l := linkFor(f.tc, at)
		require.NoError(t, f.store.SaveLink(ctx, l))
		got, err := f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, l, got)

		again := linkFor(f.tc, at.Add(time.Hour))
		again.Sheets.Wallets = "w2"
		require.NoError(t, again.Enable(at.Add(time.Hour)))
		require.NoError(t, f.store.SaveLink(ctx, again))
		got, err = f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, l.ID, got.ID, "an upsert by project keeps the first row's id")
		require.Equal(t, "w2", got.Sheets.Wallets)
		require.True(t, got.Enabled)
		on, err = f.store.LinkEnabled(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.True(t, on)
	})

	t.Run("a link save naming another org's project is refused", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.store.SaveLink(f.ctx(f.tc), linkFor(f.tc, at)))
		hijack := linkFor(tenant.Context{OrgID: f.other.OrgID, ProjectID: f.tc.ProjectID, UserID: f.other.UserID}, at)
		hijack.OSOrg = "attacker"
		err := f.store.SaveLink(f.ctx(f.other), hijack)
		require.True(t, opensheetsync.IsLinkNotFoundError(err), "got %v", err)
		got, err := f.store.LinkByProject(f.ctx(f.tc), f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, "acme", got.OSOrg)
	})

	t.Run("enabled links are listed per org", func(t *testing.T) {
		f := newFixture(t)
		l := linkFor(f.tc, at)
		require.NoError(t, l.Enable(at))
		require.NoError(t, f.store.SaveLink(f.ctx(f.tc), l))
		off := linkFor(tenant.Context{OrgID: f.tc.OrgID, ProjectID: f.sibling, UserID: f.tc.UserID}, at)
		require.NoError(t, f.store.SaveLink(f.ctx(f.tc), off))
		got, err := f.store.ListEnabledLinks(f.ctx(f.tc), f.tc.OrgID)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, f.tc.ProjectID, got[0].ProjectID)
		got, err = f.store.ListEnabledLinks(f.ctx(f.other), f.other.OrgID)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("mark, claim, settle, release", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at))
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, opensheetsync.Backlog{Pending: 1}, b)

		tok, thief := uuid.New(), uuid.New()
		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, at, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		require.EqualValues(t, 1, st.Version)
		require.EqualValues(t, 0, st.SyncedVersion)
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, thief, at.Add(time.Second), time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "a leased row cannot be claimed")

		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at.Add(2*time.Second)))
		settled, _, err := f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, w, 1, thief)
		require.NoError(t, err)
		require.False(t, settled, "only the lease holder settles")
		settled, dirty, err := f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, w, st.Version, tok)
		require.NoError(t, err)
		require.True(t, settled)
		require.True(t, dirty, "a mark during the push leaves the row dirty")

		st, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, at.Add(3*time.Second), time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		require.EqualValues(t, 2, st.Version)
		require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, opensheetsync.Failure{Reason: "boom", At: at.Add(4 * time.Second), Count: true, Version: st.Version}))
		b, err = f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, opensheetsync.Backlog{Pending: 1, Failing: 1}, b)
		st, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, at.Add(5*time.Second), time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "a released row is claimable again")
		require.Equal(t, 1, st.Attempts)
		require.Equal(t, "boom", st.LastError)
		settled, dirty, err = f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, w, st.Version, tok)
		require.NoError(t, err)
		require.True(t, settled)
		require.False(t, dirty)
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, at.Add(6*time.Second), time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "a clean row is not claimed")
		b, err = f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Zero(t, b)
	})

	t.Run("an expired lease can be taken over and the old holder loses the settle", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		c := ref(opensheetsync.EntityCategory, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{c}, at))
		old, next := uuid.New(), uuid.New()
		_, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, c, old, at, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, c, next, at.Add(2*time.Minute), time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		settled, _, err := f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, c, 1, old)
		require.NoError(t, err)
		require.False(t, settled)

		require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, c, old, opensheetsync.Failure{Reason: "stale", At: at.Add(2*time.Minute + time.Second), Count: true, Version: 1}))
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, c, uuid.New(), at.Add(2*time.Minute+2*time.Second), time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "the old holder's release leaves the new lease in place")
		require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, c, uuid.New(), opensheetsync.Failure{Reason: "stranger", At: at.Add(2*time.Minute + 3*time.Second), Count: true, Version: 1}))
		require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, c, next, opensheetsync.Failure{Reason: "transient", At: at.Add(2*time.Minute + 4*time.Second), Version: 1}))
		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, c, uuid.New(), at.Add(2*time.Minute+5*time.Second), time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "the holder's release frees the row")
		require.Zero(t, st.Attempts, "releases under a lost or a stranger's token charge nothing")
		require.Equal(t, "transient", st.LastError)
	})

	t.Run("another org cannot claim, settle, release or count these rows", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at))
		octx, tok := f.ctx(f.other), uuid.New()
		_, ok, err := f.store.Claim(octx, f.other.OrgID, f.tc.ProjectID, w, tok, at, time.Minute)
		require.NoError(t, err)
		require.False(t, ok)
		settled, _, err := f.store.Settle(octx, f.other.OrgID, f.tc.ProjectID, w, 1, tok)
		require.NoError(t, err)
		require.False(t, settled)
		require.NoError(t, f.store.Release(octx, f.other.OrgID, f.tc.ProjectID, w, tok, opensheetsync.Failure{Reason: "hijack", At: at, Count: true, Version: 1, RetryAfter: at.Add(time.Hour)}))
		b, err := f.store.Backlog(octx, f.other.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Zero(t, b)

		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), at.Add(time.Second), time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "the row is still unleased and has no retry_after")
		require.EqualValues(t, 1, st.Version)
		require.EqualValues(t, 0, st.SyncedVersion)
		require.Zero(t, st.Attempts)
		require.Empty(t, st.LastError)
	})

	t.Run("a release after a new edit does not charge that edit", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at))
		tok := uuid.New()
		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, at, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at.Add(time.Second)))
		require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, opensheetsync.Failure{
			Reason: "SHT016", At: at.Add(2 * time.Second), Count: true, Version: st.Version, RetryAfter: at.Add(time.Hour),
		}))
		got, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), at.Add(3*time.Second), time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "no retry_after holds the new edit back")
		require.EqualValues(t, 2, got.Version)
		require.Zero(t, got.Attempts, "the refusal was for version 1")
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, opensheetsync.Backlog{Pending: 1}, b)
	})

	t.Run("mark without a link fails", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		w := f.wallet(t, f.tc)
		f.transaction(t, f.tc, w, uuid.Nil, uuid.Nil)
		require.Error(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, w)}, at))
		_, err := f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityWallet, w), at)
		require.Error(t, err)
		_, err = f.store.MarkAll(ctx, f.tc.OrgID, f.tc.ProjectID, at)
		require.Error(t, err)
		n, err := f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityWallet, uuid.New()), at)
		require.NoError(t, err, "nothing to mark writes nothing")
		require.Zero(t, n)
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Zero(t, b)
	})

	t.Run("mark referencing follows the ref's entity", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := f.wallet(t, f.tc)
		cat := f.category(t, f.tc)
		tx := f.transaction(t, f.tc, w, uuid.Nil, cat)
		n, err := f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityCategory, w), at)
		require.NoError(t, err)
		require.Zero(t, n, "a wallet id named as a category matches nothing")
		n, err = f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityWallet, cat), at)
		require.NoError(t, err)
		require.Zero(t, n, "a category id named as a wallet matches nothing")
		n, err = f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityTransaction, tx), at)
		require.NoError(t, err)
		require.Zero(t, n, "a transaction cascades to nothing")
	})

	t.Run("deleted is sticky", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		id := uuid.New()
		gone := opensheetsync.Ref{Entity: opensheetsync.EntityTransaction, ID: id, Deleted: true}
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{gone}, at))
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityTransaction, id)}, at))
		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, gone, uuid.New(), at, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, st.Deleted)
		require.EqualValues(t, 2, st.Version)
	})

	t.Run("list dirty honours the cutoff, leases, limit and order, per org", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		a, b, c := ref(opensheetsync.EntityWallet, uuid.New()), ref(opensheetsync.EntityWallet, uuid.New()), ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{a}, at))
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{b}, at.Add(time.Second)))
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{c}, at.Add(10*time.Minute)))
		_, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, b, uuid.New(), at.Add(2*time.Second), time.Hour)
		require.NoError(t, err)
		require.True(t, ok)

		got, err := f.store.ListDirty(ctx, f.tc.OrgID, f.tc.ProjectID, at.Add(5*time.Minute), at.Add(5*time.Minute), 10)
		require.NoError(t, err)
		require.Equal(t, []opensheetsync.Ref{a}, got, "b is leased, c is newer than the cutoff")
		got, err = f.store.ListDirty(ctx, f.tc.OrgID, f.tc.ProjectID, at.Add(time.Hour), at.Add(2*time.Hour), 1)
		require.NoError(t, err)
		require.Equal(t, []opensheetsync.Ref{a}, got, "oldest first, capped by limit")
		got, err = f.store.ListDirty(f.ctx(f.other), f.other.OrgID, f.tc.ProjectID, at.Add(time.Hour), at.Add(2*time.Hour), 10)
		require.NoError(t, err)
		require.Empty(t, got, "another org never sees these rows")
	})

	t.Run("mark all and mark referencing read the project's own rows", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w1, w2 := f.wallet(t, f.tc), f.wallet(t, f.tc)
		cat := f.category(t, f.tc)
		f.transaction(t, f.tc, w1, uuid.Nil, cat)
		f.transaction(t, f.tc, w2, w1, uuid.Nil)
		f.transaction(t, f.tc, w2, uuid.Nil, uuid.Nil)
		sibling := tenant.Context{OrgID: f.tc.OrgID, ProjectID: f.sibling, UserID: f.tc.UserID}
		f.wallet(t, sibling)

		n, err := f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityWallet, w1), at)
		require.NoError(t, err)
		require.EqualValues(t, 2, n, "w1 as wallet and as to_wallet")
		n, err = f.store.MarkReferencing(ctx, f.tc.OrgID, f.tc.ProjectID, ref(opensheetsync.EntityCategory, cat), at)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		n, err = f.store.MarkAll(ctx, f.tc.OrgID, f.tc.ProjectID, at)
		require.NoError(t, err)
		require.EqualValues(t, 6, n, "2 wallets, 1 category, 3 transactions; the sibling project's wallet is not touched")
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.EqualValues(t, 6, b.Pending)
	})

	t.Run("MarkAll re-marks a state row whose entity is gone", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		sibling := tenant.Context{OrgID: f.tc.OrgID, ProjectID: f.sibling, UserID: f.tc.UserID}
		require.NoError(t, f.store.SaveLink(f.ctx(sibling), linkFor(sibling, at)))
		w := f.wallet(t, f.tc)
		cat := f.category(t, f.tc)
		tx := f.transaction(t, f.tc, w, uuid.Nil, cat)
		elsewhere := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.sibling, []opensheetsync.Ref{elsewhere}, at))
		n, err := f.store.MarkAll(ctx, f.tc.OrgID, f.tc.ProjectID, at)
		require.NoError(t, err)
		require.EqualValues(t, 3, n)
		refs := []opensheetsync.Ref{ref(opensheetsync.EntityTransaction, tx), ref(opensheetsync.EntityCategory, cat), ref(opensheetsync.EntityWallet, w)}
		for _, r := range refs {
			tok := uuid.New()
			st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, r, tok, at, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			_, _, err = f.store.Settle(ctx, f.tc.OrgID, f.tc.ProjectID, r, st.Version, tok)
			require.NoError(t, err)
		}
		tok := uuid.New()
		st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.sibling, elsewhere, tok, at, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		_, _, err = f.store.Settle(ctx, f.tc.OrgID, f.sibling, elsewhere, st.Version, tok)
		require.NoError(t, err)

		f.remove(t, f.tc, opensheetsync.EntityTransaction, tx)
		f.remove(t, f.tc, opensheetsync.EntityCategory, cat)
		n, err = f.store.MarkAll(ctx, f.tc.OrgID, f.tc.ProjectID, at.Add(time.Second))
		require.NoError(t, err)
		require.EqualValues(t, 3, n, "the live wallet plus the two rows whose entity is gone")
		for _, want := range []struct {
			ref     opensheetsync.Ref
			deleted bool
		}{{refs[0], true}, {refs[1], true}, {refs[2], false}} {
			st, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, want.ref, uuid.New(), at.Add(2*time.Second), time.Minute)
			require.NoError(t, err)
			require.True(t, ok, "%s is dirty again", want.ref.Entity)
			require.EqualValues(t, 2, st.Version)
			require.Equal(t, want.deleted, st.Deleted, "%s deleted", want.ref.Entity)
		}
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.sibling, elsewhere, uuid.New(), at.Add(2*time.Second), time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "another project's state row is left alone")
	})

	t.Run("delete link cascades its state", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{ref(opensheetsync.EntityWallet, uuid.New())}, at))
		require.True(t, opensheetsync.IsLinkNotFoundError(f.store.DeleteLink(f.ctx(f.other), f.other.OrgID, f.tc.ProjectID)),
			"another org cannot remove this link")
		require.NoError(t, f.store.DeleteLink(ctx, f.tc.OrgID, f.tc.ProjectID))
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Zero(t, b.Pending)
		require.True(t, opensheetsync.IsLinkNotFoundError(f.store.DeleteLink(ctx, f.tc.OrgID, f.tc.ProjectID)))
	})

	t.Run("outcomes: success resets, the third failure in a row disables and unverifies", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		l := linkFor(f.tc, at)
		require.NoError(t, l.Enable(at))
		require.NoError(t, f.store.SaveLink(ctx, l))
		for i := 1; i < opensheetsync.DisableAfter; i++ {
			off, err := f.store.SaveOutcome(ctx, f.tc.OrgID, f.tc.ProjectID, opensheetsync.Outcome{At: at, Err: "nope", LinkUpdatedAt: l.UpdatedAt})
			require.NoError(t, err)
			require.False(t, off)
		}
		_, err := f.store.SaveOutcome(ctx, f.tc.OrgID, f.tc.ProjectID, opensheetsync.Outcome{At: at, LinkUpdatedAt: l.UpdatedAt})
		require.NoError(t, err)
		got, err := f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Zero(t, got.FailureStreak)
		require.Equal(t, at, *got.LastSyncedAt)

		var off bool
		for range opensheetsync.DisableAfter {
			off, err = f.store.SaveOutcome(ctx, f.tc.OrgID, f.tc.ProjectID, opensheetsync.Outcome{At: at, Err: "sheet gone", LinkUpdatedAt: l.UpdatedAt})
			require.NoError(t, err)
		}
		require.True(t, off)
		got, err = f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.False(t, got.Enabled)
		require.Nil(t, got.VerifiedAt)
		require.True(t, got.AutoDisabled())
		require.Equal(t, "sheet gone", got.LastError)
		require.Equal(t, opensheetsync.DisableAfter, got.FailureStreak)
	})

	t.Run("an outcome for a link re-saved since the job loaded it is ignored", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		l := linkFor(f.tc, at)
		require.NoError(t, l.Enable(at))
		require.NoError(t, f.store.SaveLink(ctx, l))
		stale := l.UpdatedAt
		resaved := linkFor(f.tc, at.Add(time.Hour))
		require.NoError(t, resaved.Enable(at.Add(time.Hour)))
		require.NoError(t, f.store.SaveLink(ctx, resaved))
		for range opensheetsync.DisableAfter {
			off, err := f.store.SaveOutcome(ctx, f.tc.OrgID, f.tc.ProjectID, opensheetsync.Outcome{At: at, Err: "old sheet", LinkUpdatedAt: stale})
			require.NoError(t, err)
			require.False(t, off)
		}
		got, err := f.store.LinkByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.True(t, got.Enabled)
		require.Zero(t, got.FailureStreak)
	})

	t.Run("a refused row waits for its retry_after and stops at MaxRowAttempts until marked again", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at))
		now := at
		for i := 1; i <= opensheetsync.MaxRowAttempts; i++ {
			tok := uuid.New()
			_, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, now, time.Minute)
			require.NoError(t, err)
			require.True(t, ok, "attempt %d", i)
			retry := now.Add(opensheetsync.RowBackoff(i))
			require.NoError(t, f.store.Release(ctx, f.tc.OrgID, f.tc.ProjectID, w, tok, opensheetsync.Failure{Reason: "SHT014", At: now, Count: true, Version: 1, RetryAfter: retry}))
			_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), now.Add(time.Second), time.Minute)
			require.NoError(t, err)
			require.False(t, ok, "held back until retry_after")
			got, err := f.store.ListDirty(ctx, f.tc.OrgID, f.tc.ProjectID, now.Add(time.Hour), now.Add(time.Second), 10)
			require.NoError(t, err)
			require.Empty(t, got, "the reconciler does not list it either")
			now = retry.Add(time.Second)
		}
		_, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), now.Add(time.Hour), time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "given up after MaxRowAttempts")
		b, err := f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, opensheetsync.Backlog{GivenUp: 1}, b, "a given-up row is no longer pending")
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, now))
		_, ok, err = f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), now, time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "a new edit gives the row fresh attempts")
		b, err = f.store.Backlog(ctx, f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err)
		require.Equal(t, opensheetsync.Backlog{Pending: 1}, b)
	})

	t.Run("concurrent claims: exactly one wins", func(t *testing.T) {
		f := newFixture(t)
		ctx := f.ctx(f.tc)
		require.NoError(t, f.store.SaveLink(ctx, linkFor(f.tc, at)))
		w := ref(opensheetsync.EntityWallet, uuid.New())
		require.NoError(t, f.store.Mark(ctx, f.tc.OrgID, f.tc.ProjectID, []opensheetsync.Ref{w}, at))
		var won atomic.Int32
		var g errgroup.Group
		for range 8 {
			g.Go(func() error {
				_, ok, err := f.store.Claim(ctx, f.tc.OrgID, f.tc.ProjectID, w, uuid.New(), at, time.Minute)
				if ok {
					won.Add(1)
				}
				return err
			})
		}
		require.NoError(t, g.Wait())
		require.EqualValues(t, 1, won.Load())
	})
}
