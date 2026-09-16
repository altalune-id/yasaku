package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
	mcprt "altalune.id/yasaku/mcp"
)

const bearerScheme = "bearer"

// auth verifies the bearer token, binds the principal to the request context and calls next.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			s.deny(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(session.PrincipalInto(r.Context(), p)))
	})
}

func (s *Server) authenticate(r *http.Request) (session.Principal, error) {
	raw, err := bearerToken(r.Header.Get("Authorization"))
	if err != nil {
		return session.Principal{}, err
	}
	if s.Verifier == nil {
		return session.Principal{}, &UnauthenticatedError{Reason: "no token verifier is configured"}
	}
	ctx := r.Context()
	p, err := s.Verifier.Verify(ctx, raw)
	if err != nil {
		s.log.WarnContext(ctx, "mcp: bearer token rejected", slog.Any("error", err))
		return session.Principal{}, &UnauthenticatedError{Reason: "the bearer token was rejected"}
	}
	u, err := s.users.ByIDP(ctx, p.IDPIssuer, p.IDPSubject)
	if err != nil {
		if user.IsNotFoundError(err) {
			return session.Principal{}, &UnknownUserError{Subject: p.IDPSubject}
		}
		return session.Principal{}, err
	}
	p.UserID = u.ID
	p.Source = session.SourceToken
	if p.Email == "" {
		p.Email = u.Email
	}
	if p.Name == "" {
		p.Name = u.Name
	}
	p.IsAdmin = u.IsAdmin
	p.Locale = u.Locale
	return p, nil
}

func bearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", &UnauthenticatedError{Reason: "the Authorization header is missing"}
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) || strings.TrimSpace(token) == "" {
		return "", &UnauthenticatedError{Reason: "Authorization must use the Bearer scheme"}
	}
	return strings.TrimSpace(token), nil
}

// Challenge writes the RFC 9728 bearer challenge with a 401 MCP001 body.
func (s *Server) Challenge(w http.ResponseWriter) {
	s.writeChallenge(context.Background(), w)
}

func (s *Server) writeChallenge(ctx context.Context, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", s.challenge)
	writeError(ctx, w, http.StatusUnauthorized, (&UnauthenticatedError{}).ToAppError())
}

func (s *Server) deny(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	switch {
	case IsUnauthenticatedError(err):
		s.writeChallenge(ctx, w)
	case IsUnknownUserError(err):
		writeError(ctx, w, http.StatusForbidden, (&UnknownUserError{}).ToAppError())
	case IsNotMemberError(err):
		writeError(ctx, w, http.StatusForbidden, (&NotMemberError{}).ToAppError())
	default:
		s.log.ErrorContext(ctx, "mcp: authentication failed unexpectedly", slog.Any("error", err))
		writeError(ctx, w, http.StatusInternalServerError, nil)
	}
}

type errorEnvelope struct {
	Error mcprt.ErrorPayload `json:"error"`
}

func writeError(ctx context.Context, w http.ResponseWriter, status int, ae *apperror.AppError) {
	payload := mcprt.ErrorPayload{Code: mcprt.CodeUnexpected, Message: "unexpected error"}
	if ae != nil {
		payload = mcprt.ErrorPayload{Code: ae.Code(), Message: ae.Message(), Meta: metaOf(ae)}
	}
	body, err := json.Marshal(errorEnvelope{Error: withContext(ctx, payload)})
	if err != nil {
		body = []byte(`{"error":{"code":"` + mcprt.CodeUnexpected + `","message":"unexpected error"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
