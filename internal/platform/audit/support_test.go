package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

func TestRecord_StaffActorKindInSupportSession(t *testing.T) {
	rec, pool, _ := newSQLiteRecorder(t)
	acme, own := uuid.New(), uuid.New()
	staff := session.Principal{UserID: uuid.New(), Email: "staff@example.com",
		Support: &session.Support{OrgID: acme, Reason: "ticket 42", Until: time.Now().Add(time.Hour)}}

	record := func(orgID uuid.UUID) audit.Entry {
		t.Helper()
		ctx := session.PrincipalInto(tenant.Into(context.Background(), tenant.Context{OrgID: orgID}), staff)
		require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
			return rec.Record(ctx, audit.Event{Action: "support.entered"})
		}))
		entries, err := rec.List(ctx, audit.Filter{})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		return entries[0]
	}

	got := record(acme)
	assert.Equal(t, audit.ActorStaff, got.ActorKind, "staff acting in the supported org are recorded as staff")
	assert.Equal(t, staff.UserID, got.ActorID)
	assert.Equal(t, "staff@example.com", got.ActorEmail)

	assert.Equal(t, audit.ActorUser, record(own).ActorKind, "outside the supported org staff act as themselves")
}
