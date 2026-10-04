package fakes_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func TestOpensheetSync_CallsLogsEveryStoreCallInOrder(t *testing.T) {
	f := fakes.NewOpensheetSync()
	ctx := t.Context()
	org, project, at := uuid.New(), uuid.New(), time.Now()
	l := opensheetsync.NewLink(org, project, uuid.New(), uuid.Nil, at)
	require.NoError(t, f.SaveLink(ctx, l))
	_, err := f.LinkEnabled(ctx, org, project)
	require.NoError(t, err)
	_, err = f.MarkReferencing(ctx, org, project, opensheetsync.Ref{Entity: opensheetsync.EntityWallet, ID: uuid.New()}, at)
	require.NoError(t, err)
	require.NoError(t, f.Mark(ctx, org, project, []opensheetsync.Ref{{Entity: opensheetsync.EntityWallet, ID: uuid.New()}}, at))
	require.Equal(t, []string{"SaveLink", "LinkEnabled", "MarkReferencing", "Mark"}, f.Calls())
}
