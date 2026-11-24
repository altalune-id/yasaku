package auth

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/session"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/auth")

// Service is the driving port for login use cases.
type Service struct {
	local      *LocalLogin
	oidc       *OIDCLogin
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	stamp      SignInStampFn
	gate       SignInGateFn
}

// SignInGateFn refuses a user who may not sign in, such as a suspended one.
type SignInGateFn func(ctx context.Context, userID uuid.UUID) error

// WithSignInGate refuses every sign-in that fn refuses, whichever way the user signed in.
func WithSignInGate(fn SignInGateFn) ServiceOption {
	return func(s *Service) { s.gate = fn }
}

// SignInStampFn records that a user signed in.
type SignInStampFn func(ctx context.Context, userID uuid.UUID) error

// ServiceOption tunes Service construction.
type ServiceOption func(*Service)

// WithSignInStamp records every successful sign-in through fn.
func WithSignInStamp(fn SignInStampFn) ServiceOption {
	return func(s *Service) { s.stamp = fn }
}

// NewService binds the service to its login workflows.
func NewService(local *LocalLogin, oidc *OIDCLogin, log *slog.Logger, unexpected apperror.UnexpectedFunc, opts ...ServiceOption) *Service {
	s := &Service{
		local:      local,
		oidc:       oidc,
		log:        log.With("module", "auth"),
		unexpected: unexpected,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// LocalConfigured reports whether the local-login form should be exposed.
func (s *Service) LocalConfigured() bool {
	return s.local != nil && s.local.Configured()
}

// LoginLocal authenticates the caller against the genesis credentials.
func (s *Service) LoginLocal(ctx context.Context, creds Credentials) (session.Principal, error) {
	ctx, span := tracer.Start(ctx, "auth.LoginLocal")
	defer span.End()
	if s.local == nil {
		return session.Principal{}, &InvalidCredentialsError{}
	}
	p, err := s.local.Execute(ctx, creds)
	return s.finish(ctx, p, err)
}

// LoginOIDC authenticates the caller against the third-party identity claims.
func (s *Service) LoginOIDC(ctx context.Context, claims OIDCClaims) (session.Principal, error) {
	ctx, span := tracer.Start(ctx, "auth.LoginOIDC")
	defer span.End()
	if s.oidc == nil {
		return session.Principal{}, &OIDCUnavailableError{}
	}
	p, err := s.oidc.Execute(ctx, claims)
	return s.finish(ctx, p, err)
}

// SECURITY: the gate runs after every workflow, so no way of signing in skips it.
func (s *Service) finish(ctx context.Context, p session.Principal, err error) (session.Principal, error) {
	if err != nil || p.UserID == uuid.Nil {
		return p, err
	}
	if s.gate != nil {
		if gerr := s.gate(ctx, p.UserID); gerr != nil {
			return session.Principal{}, gerr
		}
	}
	if s.stamp != nil {
		if serr := s.stamp(ctx, p.UserID); serr != nil {
			s.log.WarnContext(ctx, "auth: stamp sign-in", "user_id", p.UserID.String(), "err", serr)
		}
	}
	return p, nil
}
