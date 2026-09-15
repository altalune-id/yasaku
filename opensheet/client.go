// Package opensheet is a client for the opensheet HTTP data plane.
package opensheet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"altalune.id/yasaku/httpclient"
)

// Config configures a Client.
type Config struct {
	BaseURL           string
	Org, Project      string
	Token             string
	AllowPrivateHosts bool
	Timeout           time.Duration
	Retry             httpclient.RetryPolicy
	UserAgent         string
}

// Client talks to one opensheet project.
type Client struct {
	base     string
	safe     *resty.Client
	replayed *resty.Client
}

// New validates cfg and builds the two underlying HTTP clients.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("opensheet: invalid BaseURL %q", cfg.BaseURL)
	}
	// SECURITY: httpclient.NewResty's SSRF guard only fires on dial, so a literal-IP BaseURL must be checked here too.
	if !cfg.AllowPrivateHosts {
		if addr, perr := netip.ParseAddr(u.Hostname()); perr == nil && httpclient.IsPrivateOrLocal(addr) {
			return nil, fmt.Errorf("opensheet: BaseURL %q is a private host; set AllowPrivateHosts to allow it", cfg.BaseURL)
		}
	}
	if cfg.Org == "" || cfg.Project == "" {
		return nil, errors.New("opensheet: Org and Project are required")
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
	mk := func(nonIdempotent bool) *resty.Client {
		r := cfg.Retry
		r.RetryNonIdempotent = nonIdempotent
		c := httpclient.NewResty(httpclient.RestyOptions{
			Timeout:           cfg.Timeout,
			UserAgent:         cfg.UserAgent,
			AllowPrivateHosts: cfg.AllowPrivateHosts,
			ResponseBodyLimit: 8 << 20,
			Retry:             r,
		})
		if cfg.Token != "" {
			c.SetAuthToken(cfg.Token)
		}
		return c
	}
	base := strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/orgs/" + url.PathEscape(cfg.Org) + "/projects/" + url.PathEscape(cfg.Project)
	return &Client{base: base, safe: mk(false), replayed: mk(true)}, nil
}

func (c *Client) sheetURL(slug string, parts ...string) string {
	u := c.base + "/sheets/" + url.PathEscape(slug)
	for _, p := range parts {
		u += "/" + url.PathEscape(p)
	}
	return u
}

func (c *Client) do(ctx context.Context, cl *resty.Client, method, u string, body any, headers map[string]string, out any) (*resty.Response, error) {
	req := cl.R().SetContext(ctx).SetHeaders(headers)
	if body != nil {
		req.SetBody(body)
	}
	if out != nil {
		req.SetResult(out)
	}
	resp, err := req.Execute(method, u)
	if err != nil {
		return nil, fmt.Errorf("opensheet: %s %s: %w", method, u, err)
	}
	if resp.IsError() {
		return resp, fromResponse(resp)
	}
	return resp, nil
}
