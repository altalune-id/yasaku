package opensheet

type writeOpts struct {
	idempotencyKey string
	numeric        []string
	ifMatch        ETag
}

// WriteOption configures a single write call.
type WriteOption func(*writeOpts)

// WithIdempotencyKey sends an Idempotency-Key header so a retried CreateRow replays the same result.
func WithIdempotencyKey(k string) WriteOption {
	return func(o *writeOpts) { o.idempotencyKey = k }
}

// WithNumericColumns marks the given row columns as numeric so they are sent as JSON numbers.
func WithNumericColumns(cols ...string) WriteOption {
	return func(o *writeOpts) { o.numeric = cols }
}

// WithIfMatch sends an If-Match header so the write fails with PreconditionFailedError if the row changed.
func WithIfMatch(e ETag) WriteOption {
	return func(o *writeOpts) { o.ifMatch = e }
}

func collectOpts(opts []WriteOption) writeOpts {
	var o writeOpts
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func headersFor(o writeOpts) map[string]string {
	h := map[string]string{}
	if o.idempotencyKey != "" {
		h["Idempotency-Key"] = o.idempotencyKey
	}
	if o.ifMatch != "" {
		h["If-Match"] = string(o.ifMatch)
	}
	return h
}
