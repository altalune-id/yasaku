package dataplane

import (
	"maps"
	"slices"

	"altalune.id/yasaku/internal/platform/surfaces"
)

// VerbTable names the domain verb every route this surface registers exposes, keyed by method and path suffix.
func VerbTable() map[string]surfaces.Verb {
	return map[string]surfaces.Verb{
		"GET":                    {Module: "blog", Aggregate: "post", Operation: "list"},
		"GET /{slug}":            {Module: "blog", Aggregate: "post", Operation: "get"},
		"POST":                   {Module: "blog", Aggregate: "post", Operation: "create"},
		"PUT /{slug}":            {Module: "blog", Aggregate: "post", Operation: "replace"},
		"PATCH /{slug}":          {Module: "blog", Aggregate: "post", Operation: "update"},
		"DELETE /{slug}":         {Module: "blog", Aggregate: "post", Operation: "delete"},
		"POST /{slug}/publish":   {Module: "blog", Aggregate: "post", Operation: "publish"},
		"POST /{slug}/unpublish": {Module: "blog", Aggregate: "post", Operation: "unpublish"},
	}
}

// Verbs returns the domain verbs S3 exposes, deduplicated.
func Verbs() []surfaces.Verb {
	return slices.Collect(maps.Values(VerbTable()))
}
