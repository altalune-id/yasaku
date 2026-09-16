package opensheet

import (
	"net/url"
	"strconv"
)

// Filter is a single where-clause term, rendered as "column:op" or "column:op:value".
type Filter struct {
	Column, Op, Value string
}

// String renders f in the server's where-clause syntax.
func (f Filter) String() string {
	s := f.Column + ":" + f.Op
	if f.Value != "" {
		s += ":" + f.Value
	}
	return s
}

// Query narrows, orders, and limits the rows returned by Pages or Rows.
type Query struct {
	Where []Filter
	Sort  string
	Limit int
}

func (q Query) values() url.Values {
	v := url.Values{}
	for _, f := range q.Where {
		v.Add("where", f.String())
	}
	if q.Sort != "" {
		v.Add("sort", q.Sort)
	}
	if q.Limit > 0 {
		v.Add("limit", strconv.Itoa(q.Limit))
	}
	return v
}
