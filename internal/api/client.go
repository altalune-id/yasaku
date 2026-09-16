package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	blogv1connect "altalune.id/yasaku/gen/go/blog/v1/blogv1connect"
	todov1connect "altalune.id/yasaku/gen/go/todo/v1/todov1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
)

// DefaultClientTimeout is applied to the http.Client used by NewClient.
const DefaultClientTimeout = 30 * time.Second

// Client bundles the Connect clients for every service published by api.Server.
type Client struct {
	Auth authv1connect.AuthServiceClient
	Todo todov1connect.TodoServiceClient
	Blog blogv1connect.BlogServiceClient

	Workspace   yasakuv1connect.WorkspaceServiceClient
	Ledger      yasakuv1connect.LedgerServiceClient
	Wallet      yasakuv1connect.WalletServiceClient
	Category    yasakuv1connect.CategoryServiceClient
	Transaction yasakuv1connect.TransactionServiceClient
	Period      yasakuv1connect.PeriodServiceClient
	Report      yasakuv1connect.ReportServiceClient
}

// NewClient builds a Client pointing at baseURL; a non-empty token becomes the Bearer header.
func NewClient(baseURL, token string) *Client {
	httpClient := &http.Client{Timeout: DefaultClientTimeout}
	base := strings.TrimRight(baseURL, "/") + "/api"
	opts := []connect.ClientOption{
		connect.WithInterceptors(bearerInterceptor(token)),
	}
	return &Client{
		Auth: authv1connect.NewAuthServiceClient(httpClient, base, opts...),
		Todo: todov1connect.NewTodoServiceClient(httpClient, base, opts...),
		Blog: blogv1connect.NewBlogServiceClient(httpClient, base, opts...),

		Workspace:   yasakuv1connect.NewWorkspaceServiceClient(httpClient, base, opts...),
		Ledger:      yasakuv1connect.NewLedgerServiceClient(httpClient, base, opts...),
		Wallet:      yasakuv1connect.NewWalletServiceClient(httpClient, base, opts...),
		Category:    yasakuv1connect.NewCategoryServiceClient(httpClient, base, opts...),
		Transaction: yasakuv1connect.NewTransactionServiceClient(httpClient, base, opts...),
		Period:      yasakuv1connect.NewPeriodServiceClient(httpClient, base, opts...),
		Report:      yasakuv1connect.NewReportServiceClient(httpClient, base, opts...),
	}
}

func bearerInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			return next(ctx, req)
		}
	}
}
