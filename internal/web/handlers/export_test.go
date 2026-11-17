package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
)

// WebhookErrorRuleOf returns the key, refusal flag and max of the rule matching err.
func WebhookErrorRuleOf(err error) (key string, refusal bool, maxLen int) {
	r := lookupWebhookError(err)
	return r.key, r.refusal, r.max
}

// WebhookErrorRuleCount returns how many rules the webhook error table holds.
func WebhookErrorRuleCount() int { return len(webhookErrorRules()) }

// ScopeLabelKey returns the locale key the API key pages label scope with.
func ScopeLabelKey(scope string) string { return scopeLabelKey(scope) }

// IsSignInRefusal reports whether a sign-in failure answers the 403 refusal page rather than the 500 unavailable page.
func IsSignInRefusal(err error) bool { return isRefusal(err) }

// NewProjectScope builds a scope for userID in o, for tests of helpers that take one.
func NewProjectScope(r *http.Request, o *org.Org, userID uuid.UUID) ProjectScope {
	return ProjectScope{principal: session.Principal{UserID: userID}, org: o, req: r}
}
