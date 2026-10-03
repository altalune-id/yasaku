package dataplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"altalune.id/yasaku/httpclient"
)

// ClientMountPath is the path S3 is mounted under, appended to the instance base URL.
const ClientMountPath = "/api/v1"

// DefaultClientTimeout bounds one data plane call.
const DefaultClientTimeout = 30 * time.Second

// DefaultResponseBodyLimit caps how many bytes a data plane response may yield.
const DefaultResponseBodyLimit = 8 << 20

// Post is a blog post as the data plane represents it on the wire.
type Post struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	CategoryID string `json:"categoryId"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
}

// PostInput is the mutable body of a post write; a nil field is left alone by a patch and cleared by a replace.
type PostInput struct {
	Title      *string `json:"title,omitempty"`
	Slug       *string `json:"slug,omitempty"`
	Body       *string `json:"body,omitempty"`
	CategoryID *string `json:"categoryId,omitempty"`
}

// WriteOpts carries the conditional-write headers: IfVersion becomes If-Match, IdempotencyKey makes a create replay-safe.
type WriteOpts struct {
	IfVersion      int
	IdempotencyKey string
}

// StatusError reports a data plane response whose status the client maps to no typed error.
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("dataplane: %s (%d)", e.Message, e.Status)
}

// IsStatusError reports whether err is a *StatusError.
func IsStatusError(err error) bool {
	var target *StatusError
	return errors.As(err, &target)
}

// Client calls S3, the REST data plane, with an API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient returns a Client for the yasaku instance at baseURL, authenticating with apiKey.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// SECURITY: private hosts are allowed because baseURL is operator-chosen, not caller-supplied.
		http: httpclient.New(
			httpclient.WithAllowPrivateHosts(true),
			httpclient.WithTimeout(DefaultClientTimeout),
			httpclient.WithResponseBodyLimit(DefaultResponseBodyLimit),
		),
	}
}

// ListPosts returns every post the key can read in one project.
func (c *Client) ListPosts(ctx context.Context, org, project string) ([]Post, error) {
	var out listResponse
	if err := c.call(ctx, http.MethodGet, c.postsPath(org, project), nil, WriteOpts{}, &out); err != nil {
		return nil, err
	}
	return out.Posts, nil
}

// GetPost reads one post by slug, returning the Version to pass back as WriteOpts.IfVersion.
func (c *Client) GetPost(ctx context.Context, org, project, slug string) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodGet, c.postPath(org, project, slug), nil, WriteOpts{}, &out)
	return out, err
}

// CreatePost adds a post, made replay-safe by a non-empty WriteOpts.IdempotencyKey.
func (c *Client) CreatePost(ctx context.Context, org, project string, in PostInput, opts WriteOpts) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodPost, c.postsPath(org, project), in, opts, &out)
	return out, err
}

// ReplacePost overwrites a post wholesale; WriteOpts.IfVersion is mandatory.
func (c *Client) ReplacePost(ctx context.Context, org, project, slug string, in PostInput, opts WriteOpts) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodPut, c.postPath(org, project, slug), in, opts, &out)
	return out, err
}

// PatchPost updates the fields in sets, leaving the rest alone; WriteOpts.IfVersion is mandatory.
func (c *Client) PatchPost(ctx context.Context, org, project, slug string, in PostInput, opts WriteOpts) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodPatch, c.postPath(org, project, slug), in, opts, &out)
	return out, err
}

// DeletePost removes a post; WriteOpts.IfVersion is mandatory.
func (c *Client) DeletePost(ctx context.Context, org, project, slug string, opts WriteOpts) error {
	return c.call(ctx, http.MethodDelete, c.postPath(org, project, slug), nil, opts, nil)
}

// PublishPost makes a post publicly readable where the instance allows it; WriteOpts.IfVersion is mandatory.
func (c *Client) PublishPost(ctx context.Context, org, project, slug string, opts WriteOpts) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodPost, c.postPath(org, project, slug)+"/publish", nil, opts, &out)
	return out, err
}

// UnpublishPost returns a post to draft; WriteOpts.IfVersion is mandatory.
func (c *Client) UnpublishPost(ctx context.Context, org, project, slug string, opts WriteOpts) (Post, error) {
	var out Post
	err := c.call(ctx, http.MethodPost, c.postPath(org, project, slug)+"/unpublish", nil, opts, &out)
	return out, err
}

type listResponse struct {
	Posts []Post `json:"posts"`
}

func (c *Client) postsPath(org, project string) string {
	return c.baseURL + ClientMountPath +
		"/orgs/" + url.PathEscape(org) +
		"/projects/" + url.PathEscape(project) +
		"/posts"
}

func (c *Client) postPath(org, project, slug string) string {
	return c.postsPath(org, project) + "/" + url.PathEscape(slug)
}

func (c *Client) call(ctx context.Context, method, target string, body any, opts WriteOpts, out any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if opts.IfVersion > 0 {
		req.Header.Set("If-Match", etagFor(opts.IfVersion))
	}
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errorFor(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func errorFor(status int, raw []byte) error {
	switch status {
	case http.StatusBadRequest:
		return &BadRequestError{}
	case http.StatusUnauthorized:
		return &UnauthorizedError{}
	case http.StatusNotFound:
		return &NotFoundError{}
	case http.StatusConflict:
		return &ConflictError{}
	case http.StatusPreconditionFailed:
		return &PreconditionFailedError{}
	case http.StatusPreconditionRequired:
		return &PreconditionRequiredError{}
	}
	var envelope errorBody
	_ = json.Unmarshal(raw, &envelope)
	message := envelope.Message
	if message == "" {
		message = strings.ToLower(http.StatusText(status))
	}
	if message == "" {
		message = "status " + strconv.Itoa(status)
	}
	return &StatusError{Status: status, Code: envelope.Code, Message: message}
}
