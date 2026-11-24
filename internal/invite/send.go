package invite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/mailer"
	"altalune.id/yasaku/nanoid"
)

// DefaultTTL is applied when SendRequest.TTL is zero.
const DefaultTTL = 7 * 24 * time.Hour

// TokenGenFunc mints a fresh raw token and its stored hash.
type TokenGenFunc = func() (raw, hash string, err error)

// SendRequest is the input to SendWorkflow.Execute.
type SendRequest struct {
	Email string
	Role  Role
	TTL   time.Duration
}

// SendWorkflow issues an invite: mints a token, persists the aggregate, and emails the raw token in an accept URL.
type SendWorkflow struct {
	invites    Store
	mailer     mailer.Mailer
	baseURL    string
	tokenGen   TokenGenFunc
	clock      func() time.Time
	uow        func(ctx context.Context, fn func(ctx context.Context) error) error
	limits     Limiter
	counter    MemberCounter
	audit      Auditor
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// SendOption configures a SendWorkflow.
type SendOption func(*SendWorkflow)

// WithTokenGen overrides the token generator (defaults to nanoid.NewInviteToken).
func WithTokenGen(gen TokenGenFunc) SendOption {
	return func(w *SendWorkflow) { w.tokenGen = gen }
}

// WithClock overrides the workflow clock (defaults to time.Now UTC).
func WithClock(clock func() time.Time) SendOption {
	return func(w *SendWorkflow) { w.clock = clock }
}

// NewSendWorkflow wires the workflow.
func NewSendWorkflow(invites Store, mail mailer.Mailer, baseURL string, log *slog.Logger, unexpected apperror.UnexpectedFunc, opts ...SendOption) *SendWorkflow {
	w := &SendWorkflow{
		invites:    invites,
		mailer:     mail,
		baseURL:    strings.TrimRight(baseURL, "/"),
		tokenGen:   nanoid.NewInviteToken,
		clock:      func() time.Time { return time.Now().UTC() },
		uow:        passthrough,
		log:        log,
		unexpected: unexpected,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Execute mints an invite, persists it, and emails the raw token.
func (w *SendWorkflow) Execute(ctx context.Context, req SendRequest) (*Invite, error) {
	ctx, span := tracer.Start(ctx, "invite.Send.Execute")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
	)

	if !req.Role.IsValid() {
		return nil, &InvalidRoleError{Role: string(req.Role)}
	}
	if _, err := normalizeEmail(req.Email); err != nil {
		return nil, err
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	raw, _, err := w.tokenGen()
	if err != nil {
		span.RecordError(err)
		return nil, w.unexpected(ctx, "invite.Send: tokenGen", fmt.Errorf("invite.Send: tokenGen: %w", err))
	}
	inv, err := New(NewParams{
		OrgID: tc.OrgID,
		Email: req.Email,
		Role:  req.Role,
		TTL:   ttl,
		Token: raw,
		Now:   w.clock(),
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	err = w.uow(ctx, func(ctx context.Context) error {
		if err := w.checkSeats(ctx, tc.OrgID); err != nil {
			return err
		}
		if err := w.invites.Save(ctx, inv); err != nil {
			span.RecordError(err)
			return w.unexpected(ctx, "invite.Send: save", fmt.Errorf("invite.Send: save: %w", err),
				"invite_id", inv.ID)
		}
		if err := record(ctx, w.audit, audit.Event{Action: "invite.sent", TargetKind: "invite", TargetID: inv.ID.String(),
			Detail: map[string]string{"name": inv.Email, "email": inv.Email, "role": string(inv.Role)}}); err != nil {
			return w.unexpected(ctx, "invite.Send: audit", err, "invite_id", inv.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	msg := mailer.Message{
		To:      inv.Email,
		Subject: "You've been invited",
		TextBody: fmt.Sprintf(
			"Accept your invite:\n\n%s/invites/accept?token=%s\n\nExpires at %s.",
			w.baseURL, raw, inv.ExpiresAt.Format(time.RFC3339),
		),
	}
	if err := w.mailer.Send(ctx, msg); err != nil {
		span.RecordError(err)
		return nil, w.unexpected(ctx, "invite.Send: mail", fmt.Errorf("invite.Send: mail: %w", err),
			"invite_id", inv.ID)
	}
	span.SetAttributes(attribute.String("invite.id", inv.ID.String()))
	return inv, nil
}

func (w *SendWorkflow) checkSeats(ctx context.Context, orgID uuid.UUID) error {
	if w.limits == nil {
		return nil
	}
	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	if err := w.limits.Lock(ctx, sub); err != nil {
		return w.unexpected(ctx, "invite.Send: limit lock", err, "org_id", orgID)
	}
	members, err := w.counter.CountMembers(ctx, orgID)
	if err != nil {
		return w.unexpected(ctx, "invite.Send: count members", err, "org_id", orgID)
	}
	pending, err := w.invites.ListPending(ctx, orgID)
	if err != nil {
		return w.unexpected(ctx, "invite.Send: count pending", err, "org_id", orgID)
	}
	open := countUnexpired(pending, w.clock())
	err = w.limits.Check(ctx, limits.OrgMembers, sub, members+open)
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok {
		return err
	}
	re.Pending = open
	if re.Name, err = w.counter.OrgName(ctx, orgID); err != nil {
		return w.unexpected(ctx, "invite.Send: org name", err, "org_id", orgID)
	}
	return re
}

func countUnexpired(invites []*Invite, now time.Time) int64 {
	var n int64
	for _, inv := range invites {
		if inv.ExpiresAt.After(now) {
			n++
		}
	}
	return n
}

func (w *SendWorkflow) seats(ctx context.Context, orgID uuid.UUID) (Seats, error) {
	if w.limits == nil {
		return Seats{Max: limits.Unlimited}, nil
	}
	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	maxSeats, err := w.limits.Effective(ctx, limits.OrgMembers, sub)
	if err != nil {
		return Seats{}, w.unexpected(ctx, "invite.Seats: limit", err, "org_id", orgID)
	}
	members, err := w.counter.CountMembers(ctx, orgID)
	if err != nil {
		return Seats{}, w.unexpected(ctx, "invite.Seats: count members", err, "org_id", orgID)
	}
	pending, err := w.invites.ListPending(ctx, orgID)
	if err != nil {
		return Seats{}, w.unexpected(ctx, "invite.Seats: count pending", err, "org_id", orgID)
	}
	open := countUnexpired(pending, w.clock())
	return Seats{Used: members + open, Max: maxSeats, Pending: open}, nil
}
