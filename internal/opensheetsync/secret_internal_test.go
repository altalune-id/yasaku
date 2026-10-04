package opensheetsync

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/sealer"
)

func testSealer(t *testing.T) sealer.Sealer {
	t.Helper()
	key, err := sealer.GenerateKey()
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)
	return sl
}

func TestKey_SealsBoundToItsOrgAndProject(t *testing.T) {
	sl := testSealer(t)
	l := NewLink(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, time.Now())
	sealed, err := sealKey(sl, l.OrgID, l.ProjectID, "osk_live_secret")
	require.NoError(t, err)
	require.NotContains(t, string(sealed), "osk_live_secret")
	l.APIKeySealed = sealed
	got, err := openKey(sl, l)
	require.NoError(t, err)
	require.Equal(t, "osk_live_secret", got)

	moved := *l
	moved.ProjectID = uuid.New()
	_, err = openKey(sl, &moved)
	require.Error(t, err, "a sealed key copied to another project must not open")

	otherOrg := *l
	otherOrg.OrgID = uuid.New()
	_, err = openKey(sl, &otherOrg)
	require.Error(t, err, "a sealed key copied to another org must not open")

	_, err = openKey(testSealer(t), l)
	require.True(t, sealer.IsOpenFailedError(err), "a rotated encryption key cannot open the old key: %v", err)
}

func TestPayload_RoundTripsAndRefusesAnUnknownEntity(t *testing.T) {
	p := payloadOf(uuid.New(), []Ref{{Entity: EntityWallet, ID: uuid.New(), Deleted: true}})
	refs, err := p.refs()
	require.NoError(t, err)
	require.Equal(t, EntityWallet, refs[0].Entity)
	require.False(t, refs[0].Deleted, "the payload names rows; the state row says whether one is deleted")
	p.Refs[0].Entity = "period"
	_, err = p.refs()
	require.Error(t, err)
}
