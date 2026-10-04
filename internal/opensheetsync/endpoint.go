package opensheetsync

import (
	"cmp"
	"time"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/opensheet"
)

const defaultSheetTimeout = 8 * time.Second

// Endpoint is the one opensheet server the deployment names in opensheet.baseURL; a tenant never chooses the host.
type Endpoint struct {
	BaseURL           string
	AllowPrivateHosts bool
	Timeout           time.Duration
}

// Validate reports a base URL that every Test and sync would refuse, without contacting opensheet. NOTE: a private host name is not resolved here, because DNS can change after boot; the dial guard refuses it at runtime and the Test and the sync report it as OSL016.
func (e Endpoint) Validate() error {
	_, err := e.client("validate", "validate", "")
	return err
}

// NOTE: one attempt per call: the queue retries a sync job, and an inline run must stay inside its 20s budget.
func (e Endpoint) client(osOrg, osProject, key string) (*opensheet.Client, error) {
	return opensheet.New(e.config(osOrg, osProject, key))
}

func (e Endpoint) config(osOrg, osProject, key string) opensheet.Config {
	return opensheet.Config{
		BaseURL:           e.BaseURL,
		Org:               osOrg,
		Project:           osProject,
		Token:             key,
		AllowPrivateHosts: e.AllowPrivateHosts,
		Timeout:           cmp.Or(e.Timeout, defaultSheetTimeout),
		Retry:             httpclient.RetryPolicy{MaxAttempts: 1},
		UserAgent:         "yasaku-opensheetsync/1",
	}
}
