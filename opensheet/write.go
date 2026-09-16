package opensheet

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-resty/resty/v2"
)

// SheetCapabilities describes what a sheet supports and whether it satisfies the opensheet write contract.
type SheetCapabilities struct {
	Columns           []string `json:"columns"`
	IDColumn          bool     `json:"idColumn"`
	SoftDelete        bool     `json:"softDelete"`
	Writable          bool     `json:"writable"`
	SatisfiesContract bool     `json:"satisfiesContract"`
	ContractReason    string   `json:"contractReason"`
	RowCount          int64    `json:"rowCount"`
	Generation        int64    `json:"generation"`
}

const maxBatchRows = 500

func numericSetOf(numeric []string) map[string]bool {
	set := make(map[string]bool, len(numeric))
	for _, col := range numeric {
		set[col] = true
	}
	return set
}

func convertRow(row Row, numericSet map[string]bool) (map[string]any, error) {
	body := make(map[string]any, len(row))
	for k, v := range row {
		if !numericSet[k] {
			body[k] = v
			continue
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, &ValidationError{Code: "client", Message: "column " + k + " is not numeric: " + err.Error()}
		}
		body[k] = n
	}
	return body, nil
}

func rowBody(row Row, numeric []string) (map[string]any, error) {
	body, err := convertRow(row, numericSetOf(numeric))
	if err != nil {
		return nil, err
	}
	if len(numeric) > 0 {
		body["numeric_columns"] = numeric
	}
	return body, nil
}

func (c *Client) writeClient(o writeOpts) *resty.Client {
	if o.idempotencyKey != "" {
		return c.replayed
	}
	return c.safe
}

// CreateRow creates one row and returns the stored row with its ETag.
func (c *Client) CreateRow(ctx context.Context, slug string, row Row, opts ...WriteOption) (Row, ETag, error) {
	o := collectOpts(opts)
	body, err := rowBody(row, o.numeric)
	if err != nil {
		return nil, "", err
	}
	var out Row
	resp, err := c.do(ctx, c.writeClient(o), http.MethodPost, c.sheetURL(slug), body, headersFor(o), &out)
	if err != nil {
		return nil, "", annotate(err, slug, "")
	}
	return out, ETag(resp.Header().Get("ETag")), nil
}

// CreateRows creates up to 500 rows in one batch and returns their assigned ids.
func (c *Client) CreateRows(ctx context.Context, slug string, rows []Row, opts ...WriteOption) ([]string, error) {
	if len(rows) > maxBatchRows {
		return nil, &ValidationError{Code: "client", Message: "batch exceeds 500 rows"}
	}
	o := collectOpts(opts)
	numericSet := numericSetOf(o.numeric)
	converted := make([]map[string]any, len(rows))
	for i, row := range rows {
		body, err := convertRow(row, numericSet)
		if err != nil {
			return nil, err
		}
		converted[i] = body
	}
	body := map[string]any{"rows": converted}
	if len(o.numeric) > 0 {
		body["numeric_columns"] = o.numeric
	}
	var out struct {
		IDs []string `json:"ids"`
	}
	_, err := c.do(ctx, c.writeClient(o), http.MethodPost, c.sheetURL(slug, "rows", "batch"), body, headersFor(o), &out)
	if err != nil {
		return nil, annotate(err, slug, "")
	}
	return out.IDs, nil
}

// ReplaceRow overwrites row id with row and returns the stored result with its ETag.
func (c *Client) ReplaceRow(ctx context.Context, slug, id string, row Row, opts ...WriteOption) (Row, ETag, error) {
	o := collectOpts(opts)
	body, err := rowBody(row, o.numeric)
	if err != nil {
		return nil, "", err
	}
	var out Row
	resp, err := c.do(ctx, c.safe, http.MethodPut, c.sheetURL(slug, id), body, headersFor(o), &out)
	if err != nil {
		return nil, "", annotate(err, slug, id)
	}
	return out, ETag(resp.Header().Get("ETag")), nil
}

// PatchRow merges patch into row id and returns the stored result with its ETag.
func (c *Client) PatchRow(ctx context.Context, slug, id string, patch Row, opts ...WriteOption) (Row, ETag, error) {
	o := collectOpts(opts)
	body, err := rowBody(patch, o.numeric)
	if err != nil {
		return nil, "", err
	}
	var out Row
	resp, err := c.do(ctx, c.safe, http.MethodPatch, c.sheetURL(slug, id), body, headersFor(o), &out)
	if err != nil {
		return nil, "", annotate(err, slug, id)
	}
	return out, ETag(resp.Header().Get("ETag")), nil
}

// DeleteRow deletes row id.
func (c *Client) DeleteRow(ctx context.Context, slug, id string, opts ...WriteOption) error {
	o := collectOpts(opts)
	_, err := c.do(ctx, c.safe, http.MethodDelete, c.sheetURL(slug, id), nil, headersFor(o), nil)
	if err != nil {
		return annotate(err, slug, id)
	}
	return nil
}

// Capabilities reports what sheet slug supports and whether it satisfies the opensheet write contract.
func (c *Client) Capabilities(ctx context.Context, slug string) (SheetCapabilities, error) {
	var caps SheetCapabilities
	_, err := c.do(ctx, c.safe, http.MethodGet, c.sheetURL(slug, "capabilities"), nil, nil, &caps)
	if err != nil {
		return SheetCapabilities{}, annotate(err, slug, "")
	}
	return caps, nil
}
