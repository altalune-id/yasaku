package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
)

func TestRefuseNotManager_SaysWhichRefusalItIs(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	const role = "Only an owner or admin can do this here."
	refuse := func(err error) (bool, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		return f.Deps.RefuseNotManager(rec, httptest.NewRequest(http.MethodPost, "/", nil), err, "test.role_only", role), rec
	}

	ok, rec := refuse(&org.OrgMismatchError{OrgID: "target"})
	require.True(t, ok, "an org mismatch is refused")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "different organization")
	assert.NotContains(t, rec.Body.String(), role, "an org mismatch is not described as a role refusal")

	ok, rec = refuse(&org.NotManagerError{})
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), role)

	ok, _ = refuse(errors.New("boom"))
	assert.False(t, ok, "any other error is left to the caller")
}
