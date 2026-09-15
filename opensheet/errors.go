package opensheet

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"

	"altalune.id/yasaku/httpclient"
)

// Row is one sheet row keyed by column name.
type Row map[string]string

// ETag identifies a specific row or page revision, as returned in the ETag header.
type ETag string

// APIError is the server's error envelope for a status this client does not model more specifically.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("opensheet: %d %s: %s", e.Status, e.Code, e.Message)
}

// IsAPIError reports whether err wraps an *APIError.
func IsAPIError(err error) bool {
	_, ok := errors.AsType[*APIError](err)
	return ok
}

// NotFoundError is the server's single answer for an unknown slug, a missing or invalid credential, a key from another project, a key without the scope, or a sheet not granted to the key.
type NotFoundError struct {
	Slug, ID string
}

func (e *NotFoundError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("opensheet: sheet %q not found (or not accessible)", e.Slug)
	}
	return fmt.Sprintf("opensheet: row %q not found in sheet %q (or not accessible)", e.ID, e.Slug)
}

// IsNotFoundError reports whether err wraps a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// PreconditionFailedError reports an If-Match that no longer matches the row's current ETag.
type PreconditionFailedError struct {
	Slug, ID string
}

func (e *PreconditionFailedError) Error() string {
	return fmt.Sprintf("opensheet: row %q in sheet %q changed since the given ETag", e.ID, e.Slug)
}

// IsPreconditionFailedError reports whether err wraps a *PreconditionFailedError.
func IsPreconditionFailedError(err error) bool {
	_, ok := errors.AsType[*PreconditionFailedError](err)
	return ok
}

// StaleCursorError reports a pagination cursor that no longer matches the sheet's current generation.
type StaleCursorError struct {
	Slug string
}

func (e *StaleCursorError) Error() string {
	return fmt.Sprintf("opensheet: pagination cursor for sheet %q is stale", e.Slug)
}

// IsStaleCursorError reports whether err wraps a *StaleCursorError.
func IsStaleCursorError(err error) bool {
	_, ok := errors.AsType[*StaleCursorError](err)
	return ok
}

// RateLimitedError reports a 429 response and how long the caller should wait before retrying.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("opensheet: rate limited, retry after %s", e.RetryAfter)
}

// IsRateLimitedError reports whether err wraps a *RateLimitedError.
func IsRateLimitedError(err error) bool {
	_, ok := errors.AsType[*RateLimitedError](err)
	return ok
}

// PayloadTooLargeError reports a request body the server refused as too large.
type PayloadTooLargeError struct {
	Slug string
}

func (e *PayloadTooLargeError) Error() string {
	return fmt.Sprintf("opensheet: payload for sheet %q is too large", e.Slug)
}

// IsPayloadTooLargeError reports whether err wraps a *PayloadTooLargeError.
func IsPayloadTooLargeError(err error) bool {
	_, ok := errors.AsType[*PayloadTooLargeError](err)
	return ok
}

// ValidationError reports a request the server, or this client, rejected as invalid.
type ValidationError struct {
	Code, Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("opensheet: validation error %s: %s", e.Code, e.Message)
}

// IsValidationError reports whether err wraps a *ValidationError.
func IsValidationError(err error) bool {
	_, ok := errors.AsType[*ValidationError](err)
	return ok
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func fromResponse(resp *resty.Response) error {
	var env errorEnvelope
	_ = json.Unmarshal(resp.Body(), &env)
	code, msg := env.Error.Code, env.Error.Message
	switch resp.StatusCode() {
	case http.StatusNotFound:
		return &NotFoundError{}
	case http.StatusPreconditionFailed:
		return &PreconditionFailedError{}
	case http.StatusConflict:
		if code == "SHT036" {
			return &StaleCursorError{}
		}
	case http.StatusTooManyRequests:
		return &RateLimitedError{RetryAfter: httpclient.ParseRetryAfter(resp.Header().Get("Retry-After"))}
	case http.StatusRequestEntityTooLarge:
		return &PayloadTooLargeError{}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &ValidationError{Code: code, Message: msg}
	}
	return &APIError{Status: resp.StatusCode(), Code: code, Message: msg}
}

// NOTE: fromResponse has no slug/id in scope, so callers fill those fields in afterward via annotate.
func annotate(err error, slug, id string) error {
	if notFound, ok := errors.AsType[*NotFoundError](err); ok {
		notFound.Slug, notFound.ID = slug, id
		return err
	}
	if precondition, ok := errors.AsType[*PreconditionFailedError](err); ok {
		precondition.Slug, precondition.ID = slug, id
		return err
	}
	if staleCursor, ok := errors.AsType[*StaleCursorError](err); ok {
		staleCursor.Slug = slug
		return err
	}
	if tooLarge, ok := errors.AsType[*PayloadTooLargeError](err); ok {
		tooLarge.Slug = slug
	}
	return err
}
