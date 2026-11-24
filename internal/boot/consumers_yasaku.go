package boot

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
	webhandlers "altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/mailer"
)

func yasakuConsumerDomains() []string { return []string{"opensheetsync", "support"} }

// NOTE: the providers are always built; an unmounted module's consumer declares no job.
func yasakuConsumerProviders(s *Services, _ *slog.Logger) []queue.Provider {
	return []queue.Provider{
		opensheetsync.NewConsumer(s.OpensheetSync),
		supportNotifier{orgs: s.OrgStore, mail: s.mail},
	}
}

func supportNotifyJob() queue.Job { return queue.Job{Name: "support.notify", Version: 1} }

type ownerLister interface {
	ListMemberProfiles(ctx context.Context, orgID uuid.UUID) ([]*org.MemberProfile, error)
}

type supportNotifier struct {
	orgs ownerLister
	mail mailer.Mailer
}

func (n supportNotifier) ConsumerHandlers() []queue.Handler {
	return []queue.Handler{{Job: supportNotifyJob(), Handle: n.handle}}
}

// NOTE: a retry after a partial send mails every owner again; track per-recipient delivery if owners complain of duplicates.
func (n supportNotifier) handle(ctx context.Context, m queue.Message) error {
	notice, err := queue.Decode[webhandlers.SupportNotice](m)
	if err != nil {
		return queue.Permanent(err)
	}
	ctx = tenant.Into(ctx, tenant.Context{OrgID: notice.OrgID})
	members, err := n.orgs.ListMemberProfiles(ctx, notice.OrgID)
	if err != nil {
		return err
	}
	var errs []error
	for _, mp := range members {
		if mp.Role != org.RoleOwner || mp.Email == "" {
			continue
		}
		errs = append(errs, n.mail.Send(ctx, mailer.Message{To: mp.Email, Subject: notice.Subject, TextBody: notice.Body}))
	}
	return errors.Join(errs...)
}
