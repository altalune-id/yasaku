package controlplane

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/testutil/fakes"
)

// TestServer_HandlerOptions_EnforcesKeyScopes proves handlerOptions wires the real authn.Interceptor, not just interceptor.Auth's JWT check.
func TestServer_HandlerOptions_EnforcesKeyScopes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)

	store := fakes.NewAPIKey()
	orgID, projectID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	readKey, readPlain, err := apikey.Scheme{}.Mint(orgID, projectID, "read-only", []string{authn.ScopeYasakuRead}, nil, nil, now)
	if err != nil {
		t.Fatalf("mint read-only key: %v", err)
	}
	store.Seed(readKey)

	adminKey, adminPlain, err := apikey.Scheme{}.Mint(orgID, projectID, "admin", []string{authn.ScopeYasakuWrite}, nil, nil, now)
	if err != nil {
		t.Fatalf("mint admin key: %v", err)
	}
	store.Seed(adminKey)

	keyAuthn := apikey.NewAuthenticator(store, nil, apikey.Scheme{}, fakes.NewMembers())
	chain := authn.Chain{keyAuthn, tokens.NewAuthenticator(alwaysOKVerifier{}, authn.Scheme{Prefix: apikey.DefaultPrefix})}

	s := &Server{
		Kernel: &platform.Kernel{Log: log, Reporter: reporter},
		Authn:  chain,
	}

	// DeleteWallet requires ScopeYasakuWrite in controlplane.ScopeTable().
	const procedure = yasakuv1connect.WalletServiceDeleteWalletProcedure
	var gotPrincipal session.Principal
	handler := connect.NewUnaryHandlerSimple(
		procedure,
		func(ctx context.Context, _ *yasakuv1.DeleteWalletRequest) (*yasakuv1.DeleteWalletResponse, error) {
			gotPrincipal = session.PrincipalFrom(ctx)
			return &yasakuv1.DeleteWalletResponse{}, nil
		},
		s.handlerOptions()...,
	)
	mux := http.NewServeMux()
	mux.Handle(procedure, handler)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := connect.NewClient[yasakuv1.DeleteWalletRequest, yasakuv1.DeleteWalletResponse](http.DefaultClient, ts.URL+procedure)

	t.Run("read-only key is denied", func(t *testing.T) {
		req := connect.NewRequest(&yasakuv1.DeleteWalletRequest{})
		req.Header().Set("Authorization", "Bearer "+readPlain)

		_, err := client.CallUnary(t.Context(), req)
		if err == nil {
			t.Fatal("expected the read-only key to be denied, got a successful delete")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("code = %v, want %v (denial: %v)", connect.CodeOf(err), connect.CodePermissionDenied, err)
		}
	})

	t.Run("admin key succeeds", func(t *testing.T) {
		req := connect.NewRequest(&yasakuv1.DeleteWalletRequest{})
		req.Header().Set("Authorization", "Bearer "+adminPlain)

		if _, err := client.CallUnary(t.Context(), req); err != nil {
			t.Fatalf("expected the admin-scoped key to succeed, got: %v", err)
		}
		if gotPrincipal.Source != session.SourceAPIKey {
			t.Errorf("principal source = %q, want %q", gotPrincipal.Source, session.SourceAPIKey)
		}
		if !slices.Contains(gotPrincipal.Scopes, authn.ScopeYasakuWrite) {
			t.Errorf("principal scopes = %v, missing %q", gotPrincipal.Scopes, authn.ScopeYasakuWrite)
		}
	})
}

// TestServer_HandlerOptions_RejectsUnscopedProcedure proves a procedure absent from ScopeTable denies every API-key caller.
func TestServer_HandlerOptions_RejectsUnscopedProcedure(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)

	store := fakes.NewAPIKey()
	key, plain, err := apikey.Scheme{}.MintOrg(uuid.New(), "everything", authn.AllScopes(), apikey.ProjectGrant{All: true}, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("mint key: %v", err)
	}
	store.Seed(key)

	chain := authn.Chain{apikey.NewAuthenticator(store, nil, apikey.Scheme{}, fakes.NewMembers()), tokens.NewAuthenticator(alwaysOKVerifier{}, authn.Scheme{Prefix: apikey.DefaultPrefix})}
	s := &Server{
		Kernel: &platform.Kernel{Log: log, Reporter: reporter},
		Authn:  chain,
	}

	const procedure = "/test.UndeclaredService/DoSomething"
	handler := connect.NewUnaryHandlerSimple(
		procedure,
		func(_ context.Context, _ *yasakuv1.DeleteWalletRequest) (*yasakuv1.DeleteWalletResponse, error) {
			return &yasakuv1.DeleteWalletResponse{}, nil
		},
		s.handlerOptions()...,
	)
	mux := http.NewServeMux()
	mux.Handle(procedure, handler)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := connect.NewClient[yasakuv1.DeleteWalletRequest, yasakuv1.DeleteWalletResponse](http.DefaultClient, ts.URL+procedure)
	req := connect.NewRequest(&yasakuv1.DeleteWalletRequest{})
	req.Header().Set("Authorization", "Bearer "+plain)

	if _, err := client.CallUnary(t.Context(), req); err == nil {
		t.Fatal("expected an undeclared procedure to deny a key with every scope, got success")
	}
}

// TestServer_HandlerOptions_AcceptsAnyConfiguredKeyPrefix proves the control-plane shape gate and the resolver share one prefix.
func TestServer_HandlerOptions_AcceptsAnyConfiguredKeyPrefix(t *testing.T) {
	for _, configured := range []string{"", "key_", "ak_", "sk_live_"} {
		t.Run("prefix="+configured, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			scheme := apikey.NewScheme(configured)
			store := fakes.NewAPIKey()

			key, plain, err := scheme.Mint(uuid.New(), uuid.New(), "probe", []string{authn.ScopeYasakuWrite}, nil, nil, time.Now().UTC())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			store.Seed(key)

			keyAuthn := apikey.NewAuthenticator(store, nil, scheme, fakes.NewMembers())
			s := &Server{
				Kernel: &platform.Kernel{Log: log, Reporter: apperror.NewReporter(log, false)},
				Authn:  authn.Chain{keyAuthn},
			}

			const procedure = yasakuv1connect.WalletServiceDeleteWalletProcedure
			mux := http.NewServeMux()
			mux.Handle(procedure, connect.NewUnaryHandlerSimple(
				procedure,
				func(_ context.Context, _ *yasakuv1.DeleteWalletRequest) (*yasakuv1.DeleteWalletResponse, error) {
					return &yasakuv1.DeleteWalletResponse{}, nil
				},
				s.handlerOptions()...,
			))
			ts := httptest.NewServer(mux)
			t.Cleanup(ts.Close)

			req := connect.NewRequest(&yasakuv1.DeleteWalletRequest{})
			req.Header().Set("Authorization", "Bearer "+plain)
			client := connect.NewClient[yasakuv1.DeleteWalletRequest, yasakuv1.DeleteWalletResponse](http.DefaultClient, ts.URL+procedure)
			if _, err := client.CallUnary(t.Context(), req); err != nil {
				t.Fatalf("api.keyPrefix %q: a key this deployment minted was refused at the boundary: %v", configured, err)
			}
		})
	}
}

type scopedTokenVerifier struct{ scopes []string }

func (v scopedTokenVerifier) Verify(context.Context, string) (session.Principal, error) {
	return session.Principal{Source: session.SourceToken, UserID: uuid.New(), Scopes: v.scopes}, nil
}

// TestServer_HandlerOptions_EnforcesTokenScopes proves a token principal is held to the same scope table as a key.
func TestServer_HandlerOptions_EnforcesTokenScopes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	const procedure = yasakuv1connect.WalletServiceCreateWalletProcedure

	for _, tc := range []struct {
		name   string
		scopes []string
		want   connect.Code
	}{
		{"a token without the scope is denied", []string{authn.ScopeYasakuRead}, connect.CodePermissionDenied},
		{"a token with the scope is admitted", []string{authn.ScopeYasakuWrite}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				Kernel: &platform.Kernel{Log: log, Reporter: reporter},
				Authn:  authn.Chain{tokens.NewAuthenticator(scopedTokenVerifier{scopes: tc.scopes}, authn.Scheme{Prefix: apikey.DefaultPrefix})},
			}
			handler := connect.NewUnaryHandlerSimple(procedure,
				func(context.Context, *yasakuv1.CreateWalletRequest) (*yasakuv1.CreateWalletResponse, error) {
					return &yasakuv1.CreateWalletResponse{}, nil
				}, s.handlerOptions()...)
			mux := http.NewServeMux()
			mux.Handle(procedure, handler)
			ts := httptest.NewServer(mux)
			t.Cleanup(ts.Close)

			client := connect.NewClient[yasakuv1.CreateWalletRequest, yasakuv1.CreateWalletResponse](http.DefaultClient, ts.URL+procedure)
			req := connect.NewRequest(&yasakuv1.CreateWalletRequest{Name: "Cash", Kind: "cash"})
			req.Header().Set("Authorization", "Bearer aaa.bbb.ccc")
			_, err := client.CallUnary(t.Context(), req)
			if tc.want == 0 {
				if err != nil {
					t.Fatalf("a token holding the scope was refused: %v", err)
				}
				return
			}
			if connect.CodeOf(err) != tc.want {
				t.Fatalf("code = %v, want %v (err: %v)", connect.CodeOf(err), tc.want, err)
			}
		})
	}
}

type failingTokenVerifier struct{ err error }

func (v failingTokenVerifier) Verify(context.Context, string) (session.Principal, error) {
	return session.Principal{}, v.err
}

// TestServer_HandlerOptions_AnOutageIsUnavailableNotUnauthenticated proves a client keeps its token through an issuer or database outage.
func TestServer_HandlerOptions_AnOutageIsUnavailableNotUnauthenticated(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	resource, err := tokens.NewResource("https://app.example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	const procedure = yasakuv1connect.WalletServiceCreateWalletProcedure
	scheme := authn.Scheme{Prefix: apikey.DefaultPrefix}
	downStore := fakes.NewAPIKey()
	downStore.BySecretHashErr = errors.New("connection refused")
	keyAuthn := apikey.NewAuthenticator(downStore, nil, apikey.Scheme{}, fakes.NewMembers())
	tokenChain := func(err error) authn.Chain {
		return authn.Chain{keyAuthn, tokens.NewAuthenticator(failingTokenVerifier{err: err}, scheme)}
	}

	for _, tc := range []struct {
		name          string
		chain         authn.Chain
		bearer        string
		want          connect.Code
		wantChallenge bool
	}{
		{"an issuer outage", tokenChain(&authn.UnavailableError{Cause: errors.New("issuer unreachable")}), "aaa.bbb.ccc", connect.CodeUnavailable, false},
		{"an api key during a store outage", tokenChain(nil), apikey.DefaultPrefix + "whatever", connect.CodeUnavailable, false},
		{"a refused token", tokenChain(&tokens.InvalidTokenError{Reason: "signature"}), "aaa.bbb.ccc", connect.CodeUnauthenticated, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				Kernel:   &platform.Kernel{Log: log, Reporter: apperror.NewReporter(log, false)},
				Authn:    tc.chain,
				Resource: &resource,
			}
			mux := http.NewServeMux()
			mux.Handle(procedure, connect.NewUnaryHandlerSimple(procedure,
				func(context.Context, *yasakuv1.CreateWalletRequest) (*yasakuv1.CreateWalletResponse, error) {
					t.Fatal("the handler ran for a credential that was not verified")
					return nil, nil
				}, s.handlerOptions()...))
			ts := httptest.NewServer(mux)
			t.Cleanup(ts.Close)

			client := connect.NewClient[yasakuv1.CreateWalletRequest, yasakuv1.CreateWalletResponse](http.DefaultClient, ts.URL+procedure)
			req := connect.NewRequest(&yasakuv1.CreateWalletRequest{Name: "Cash", Kind: "cash"})
			req.Header().Set("Authorization", "Bearer "+tc.bearer)
			_, err := client.CallUnary(t.Context(), req)
			if connect.CodeOf(err) != tc.want {
				t.Fatalf("code = %v, want %v (err: %v)", connect.CodeOf(err), tc.want, err)
			}
			var cerr *connect.Error
			if !errors.As(err, &cerr) {
				t.Fatalf("err = %T, want *connect.Error", err)
			}
			if got := cerr.Meta().Get("WWW-Authenticate") != ""; got != tc.wantChallenge {
				t.Fatalf("WWW-Authenticate present = %v, want %v", got, tc.wantChallenge)
			}
		})
	}
}
