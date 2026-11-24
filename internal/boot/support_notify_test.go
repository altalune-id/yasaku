package boot

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/testutil/fakes"
	webhandlers "altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/mailer"
)

type recordingMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (m *recordingMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func TestSupportNotify_MailsEveryOwner(t *testing.T) {
	ctx := context.Background()
	orgs := fakes.NewOrg()
	o := &org.Org{ID: uuid.New(), Slug: "acme", Name: "Acme", CreatedAt: time.Now()}
	require.NoError(t, orgs.Save(ctx, o))
	for email, role := range map[string]org.Role{"a@example.com": org.RoleOwner, "b@example.com": org.RoleOwner, "c@example.com": org.RoleMember} {
		id := uuid.New()
		orgs.SetUser(id, email, email)
		require.NoError(t, orgs.SaveMembership(ctx, &org.Membership{OrgID: o.ID, UserID: id, Role: role}))
	}

	mail := &recordingMailer{}
	hs := supportNotifier{orgs: orgs, mail: mail}.ConsumerHandlers()
	require.Len(t, hs, 1)
	assert.Equal(t, supportNotifyJob(), hs[0].Job)
	data, err := json.Marshal(webhandlers.SupportNotice{OrgID: o.ID, Subject: "Altalune support viewed Acme", Body: "body"})
	require.NoError(t, err)
	require.NoError(t, hs[0].Handle(ctx, queue.Message{Name: "support.notify", Version: 1, Data: data}))

	var to []string
	for _, m := range mail.sent {
		to = append(to, m.To)
		assert.Equal(t, "Altalune support viewed Acme", m.Subject)
		assert.Equal(t, "body", m.TextBody)
	}
	assert.ElementsMatch(t, []string{"a@example.com", "b@example.com"}, to, "every built-in owner, no one else")
}
