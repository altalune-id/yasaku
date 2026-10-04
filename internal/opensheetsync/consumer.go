package opensheetsync

import (
	"context"

	"altalune.id/yasaku/internal/platform/queue"
)

// Consumer adapts *Syncer to queue.Provider.
type Consumer struct{ syncer *Syncer }

// NewConsumer binds the sync job; a nil syncer, for an unmounted module, declares no job.
func NewConsumer(s *Syncer) *Consumer { return &Consumer{syncer: s} }

// ConsumerHandlers implements queue.Provider.
func (c *Consumer) ConsumerHandlers() []queue.Handler {
	if c == nil || c.syncer == nil {
		return nil
	}
	return []queue.Handler{{
		Job: syncJob(),
		// NOTE: every failure is recorded on the link or the row and shown on the Opensheet page, so a dead letter is not an incident and needs no OnDeadLetter.
		SuppressReport: true,
		Handle:         c.handle,
	}}
}

func (c *Consumer) handle(ctx context.Context, m queue.Message) error {
	p, err := queue.Decode[syncV1](m)
	if err != nil {
		return queue.Permanent(err)
	}
	refs, err := p.refs()
	if err != nil {
		return queue.Permanent(err)
	}
	err = c.syncer.Sync(ctx, p.ProjectID, refs)
	if IsSyncRefusedError(err) || IsScopeMismatchError(err) {
		return queue.Permanent(err)
	}
	return err
}
