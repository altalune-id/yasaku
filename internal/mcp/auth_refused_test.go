package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	mcpinternal "altalune.id/yasaku/internal/mcp"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	rootmcp "altalune.id/yasaku/mcp"
)

type refusingAuthn struct{}

func (refusingAuthn) Authenticate(context.Context, string) (session.Principal, error) {
	return session.Principal{}, &authn.RefusedError{Cause: &user.SuspendedError{Contact: "help@example.com"}}
}

// TestSuspendedTokenSaysSuspended proves a suspended person's valid token gets USR008 with the contact line, not the masked 401.
func TestSuspendedTokenSaysSuspended(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("a refused principal reached the handler") })
	h := mcpinternal.Authenticate(refusingAuthn{}, authResource(t), nil)(next)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	assert.Contains(t, challenge, "resource_metadata=", "RFC 6750 Section 3: the refusal names the resource")
	assert.NotContains(t, challenge, "error=", "the token is valid, so no error value")
	var body rootmcp.ErrorPayload
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	assert.Equal(t, apperror.CodeUserSuspended, body.Code)
	assert.Equal(t, "This account is suspended. Contact help@example.com.", body.Message)
}
