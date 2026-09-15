package opensheet

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strings"
)

// Page is one page of rows returned by Pages, along with its ETag and staleness flag.
type Page struct {
	Rows  []Row
	ETag  ETag
	Stale bool
}

// Pages iterates result pages, following the Link rel="next" header until absent.
func (c *Client) Pages(ctx context.Context, slug string, q Query) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		next := c.sheetURL(slug)
		if v := q.values().Encode(); v != "" {
			next += "?" + v
		}
		for next != "" {
			var rows []Row
			resp, err := c.do(ctx, c.safe, http.MethodGet, next, nil, nil, &rows)
			if err != nil {
				yield(Page{}, annotate(err, slug, ""))
				return
			}
			p := Page{
				Rows:  rows,
				ETag:  ETag(resp.Header().Get("ETag")),
				Stale: resp.Header().Get("X-Opensheet-Stale") == "true",
			}
			if !yield(p, nil) {
				return
			}
			next = nextLink(next, resp.Header().Get("Link"))
		}
	}
}

// Rows flattens Pages into individual rows.
func (c *Client) Rows(ctx context.Context, slug string, q Query) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		for page, err := range c.Pages(ctx, slug, q) {
			if err != nil {
				yield(nil, err)
				return
			}
			for _, row := range page.Rows {
				if !yield(row, nil) {
					return
				}
			}
		}
	}
}

// Row fetches one row by id.
func (c *Client) Row(ctx context.Context, slug, id string) (Row, ETag, error) {
	var row Row
	resp, err := c.do(ctx, c.safe, http.MethodGet, c.sheetURL(slug, id), nil, nil, &row)
	if err != nil {
		return nil, "", annotate(err, slug, id)
	}
	return row, ETag(resp.Header().Get("ETag")), nil
}

func nextLink(current, link string) string {
	if link == "" {
		return ""
	}
	for _, part := range strings.Split(link, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if len(fields) < 2 {
			continue
		}
		uriPart := strings.TrimSpace(fields[0])
		if !strings.HasPrefix(uriPart, "<") || !strings.HasSuffix(uriPart, ">") {
			continue
		}
		if !hasNextRel(fields[1:]) {
			continue
		}
		raw := uriPart[1 : len(uriPart)-1]
		return resolve(current, raw)
	}
	return ""
}

func hasNextRel(attrs []string) bool {
	for _, attr := range attrs {
		attr = strings.TrimSpace(attr)
		if attr == `rel="next"` || attr == "rel=next" {
			return true
		}
	}
	return false
}

func resolve(current, raw string) string {
	base, err := url.Parse(current)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}
