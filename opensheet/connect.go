package opensheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"altalune.id/yasaku/httpclient"
)

// ControlConfig configures a Control.
type ControlConfig struct {
	BaseURL           string
	Token             string
	AllowPrivateHosts bool
	Timeout           time.Duration
	Retry             httpclient.RetryPolicy
	UserAgent         string
}

// Control calls opensheet's Connect-JSON control plane as the person the token names.
type Control struct {
	base string
	c    *resty.Client
}

// Project is an opensheet project the token reaches.
type Project struct {
	ID      string `json:"id"`
	OrgID   string `json:"orgId"`
	OrgSlug string `json:"orgSlug"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
}

// Credential is a Google credential of an opensheet project.
type Credential struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"projectId"`
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	GoogleAccountEmail string `json:"googleAccountEmail"`
}

// ProvisionTab is one sheet Provision creates: its tab title, header row and slug.
type ProvisionTab struct {
	Title   string   `json:"title"`
	Headers []string `json:"headers"`
	Slug    string   `json:"slug"`
}

// ProvisionRequest creates a spreadsheet with tabs in a project; the same IdempotencyKey replays the first result.
type ProvisionRequest struct {
	ProjectID      string         `json:"projectId"`
	CredentialID   string         `json:"credentialId"`
	Title          string         `json:"title"`
	Tabs           []ProvisionTab `json:"tabs"`
	IdempotencyKey string         `json:"idempotencyKey"`
}

// Spreadsheet is the Google spreadsheet Provision created.
type Spreadsheet struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	GoogleFileID string `json:"googleFileId"`
}

// ConnectError is a Connect error response: its code, such as unauthenticated or not_found, and message.
type ConnectError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("opensheet: %d %s: %s", e.Status, e.Code, e.Message)
}

// Refused reports a code that means the token's person cannot do this at all: unknown, not a member, or out of scope.
func (e *ConnectError) Refused() bool {
	switch e.Code {
	case "unauthenticated", "permission_denied", "not_found":
		return true
	}
	return false
}

// IsConnectError reports whether err wraps a *ConnectError.
func IsConnectError(err error) bool {
	_, ok := AsConnectError(err)
	return ok
}

// AsConnectError returns the *ConnectError err wraps.
func AsConnectError(err error) (*ConnectError, bool) {
	return errors.AsType[*ConnectError](err)
}

// NewControl validates cfg and builds the HTTP client.
func NewControl(cfg ControlConfig) (*Control, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("opensheet: invalid BaseURL: it needs a scheme and a host")
	}
	// SECURITY: httpclient.NewResty's SSRF guard only fires on dial, so a literal-IP BaseURL must be checked here too.
	if !cfg.AllowPrivateHosts {
		if addr, perr := netip.ParseAddr(u.Hostname()); perr == nil && httpclient.IsPrivateOrLocal(addr) {
			return nil, fmt.Errorf("opensheet: BaseURL %q is a private host; set AllowPrivateHosts to allow it", u.Redacted())
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "yasaku-opensheet/1"
	}
	if cfg.Retry.MaxAttempts == 0 {
		cfg.Retry = httpclient.RetryPolicy{MaxAttempts: 3}
	}
	// NOTE: every call is a read or an idempotent Provision, so a retried POST is safe.
	cfg.Retry.RetryNonIdempotent = true
	c := httpclient.NewResty(httpclient.RestyOptions{
		Timeout: cfg.Timeout, UserAgent: cfg.UserAgent, AllowPrivateHosts: cfg.AllowPrivateHosts,
		ResponseBodyLimit: 1 << 20, Retry: cfg.Retry,
	})
	if cfg.Token != "" {
		c.SetAuthToken(cfg.Token)
	}
	return &Control{base: strings.TrimRight(cfg.BaseURL, "/") + "/api/", c: c}, nil
}

// ListProjects lists the projects the token's person reaches, across every org they belong to.
func (c *Control) ListProjects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.call(ctx, "project.v1.ProjectService/ListProjects", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// ListCredentials lists the project's Google credentials.
func (c *Control) ListCredentials(ctx context.Context, projectID string) ([]Credential, error) {
	var out struct {
		Credentials []Credential `json:"credentials"`
	}
	in := struct {
		ProjectID string `json:"projectId"`
	}{projectID}
	if err := c.call(ctx, "credential.v1.CredentialService/List", in, &out); err != nil {
		return nil, err
	}
	return out.Credentials, nil
}

// Provision creates a Google spreadsheet with req's tabs and registers each tab as a sheet under its slug.
func (c *Control) Provision(ctx context.Context, req ProvisionRequest) (Spreadsheet, error) {
	var out struct {
		Spreadsheet Spreadsheet `json:"spreadsheet"`
	}
	if err := c.call(ctx, "spreadsheet.v1.SpreadsheetService/Provision", req, &out); err != nil {
		return Spreadsheet{}, err
	}
	return out.Spreadsheet, nil
}

func (c *Control) call(ctx context.Context, procedure string, in, out any) error {
	resp, err := c.c.R().SetContext(ctx).SetHeader("Content-Type", "application/json").SetBody(in).SetResult(out).Post(c.base + procedure)
	if err != nil {
		return fmt.Errorf("opensheet: %s: %w", procedure, err)
	}
	if resp.IsError() {
		ce := &ConnectError{Status: resp.StatusCode()}
		_ = json.Unmarshal(resp.Body(), ce)
		if ce.Code == "" {
			ce.Code = http.StatusText(resp.StatusCode())
		}
		return ce
	}
	return nil
}
