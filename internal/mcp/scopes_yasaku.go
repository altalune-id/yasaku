package mcp

import (
	yasakuv1mcp "altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/platform/authn"
)

func yasakuToolScopes() authn.ScopeTable {
	r, w := authn.ScopeYasakuRead, authn.ScopeYasakuWrite
	return authn.ScopeTable{
		yasakuv1mcp.ListProjectsToolName:          r,
		yasakuv1mcp.NowToolName:                   r,
		yasakuv1mcp.ListWalletsToolName:           r,
		yasakuv1mcp.GetWalletToolName:             r,
		yasakuv1mcp.WalletTotalsToolName:          r,
		yasakuv1mcp.CreateWalletToolName:          w,
		yasakuv1mcp.UpdateWalletToolName:          w,
		yasakuv1mcp.ArchiveWalletToolName:         w,
		yasakuv1mcp.AdjustBalanceToolName:         w,
		yasakuv1mcp.ListCategoriesToolName:        r,
		yasakuv1mcp.CreateCategoryToolName:        w,
		yasakuv1mcp.SeedDefaultCategoriesToolName: w,
		yasakuv1mcp.ListRecentTxToolName:          r,
		yasakuv1mcp.SearchTxToolName:              r,
		yasakuv1mcp.RecordExpenseToolName:         w,
		yasakuv1mcp.RecordIncomeToolName:          w,
		yasakuv1mcp.RecordTransferToolName:        w,
		yasakuv1mcp.RecordBatchToolName:           w,
		yasakuv1mcp.ReviseTxToolName:              w,
		yasakuv1mcp.DeleteTxToolName:              w,
		yasakuv1mcp.CurrentPeriodToolName:         r,
		yasakuv1mcp.ListPeriodsToolName:           r,
		yasakuv1mcp.PreviewCloseToolName:          r,
		yasakuv1mcp.ClosePeriodToolName:           w,
		yasakuv1mcp.ReopenPeriodToolName:          w,
		yasakuv1mcp.PeriodReportToolName:          r,
		yasakuv1mcp.CashflowReportToolName:        r,
	}
}

// YasakuFineToolScopes is the target scope per tool once yasaku:* is retired. TODO(scopes): enforce this table instead of yasakuToolScopes, add its scopes to the authn catalogue and authl, then remove yasaku:*.
func YasakuFineToolScopes() authn.ScopeTable {
	return authn.ScopeTable{
		yasakuv1mcp.ListProjectsToolName:          "ledgers:read",
		yasakuv1mcp.NowToolName:                   "ledgers:read",
		yasakuv1mcp.ListWalletsToolName:           "wallets:read",
		yasakuv1mcp.GetWalletToolName:             "wallets:read",
		yasakuv1mcp.WalletTotalsToolName:          "wallets:read",
		yasakuv1mcp.CreateWalletToolName:          "wallets:write",
		yasakuv1mcp.UpdateWalletToolName:          "wallets:write",
		yasakuv1mcp.ArchiveWalletToolName:         "wallets:write",
		yasakuv1mcp.AdjustBalanceToolName:         "wallets:write",
		yasakuv1mcp.ListCategoriesToolName:        "categories:read",
		yasakuv1mcp.CreateCategoryToolName:        "categories:write",
		yasakuv1mcp.SeedDefaultCategoriesToolName: "categories:write",
		yasakuv1mcp.ListRecentTxToolName:          "transactions:read",
		yasakuv1mcp.SearchTxToolName:              "transactions:read",
		yasakuv1mcp.RecordExpenseToolName:         "transactions:write",
		yasakuv1mcp.RecordIncomeToolName:          "transactions:write",
		yasakuv1mcp.RecordTransferToolName:        "transactions:write",
		yasakuv1mcp.RecordBatchToolName:           "transactions:write",
		yasakuv1mcp.ReviseTxToolName:              "transactions:write",
		yasakuv1mcp.DeleteTxToolName:              "transactions:admin",
		yasakuv1mcp.CurrentPeriodToolName:         "periods:read",
		yasakuv1mcp.ListPeriodsToolName:           "periods:read",
		yasakuv1mcp.PreviewCloseToolName:          "periods:read",
		yasakuv1mcp.ClosePeriodToolName:           "periods:write",
		yasakuv1mcp.ReopenPeriodToolName:          "periods:admin",
		yasakuv1mcp.PeriodReportToolName:          "reports:read",
		yasakuv1mcp.CashflowReportToolName:        "reports:read",
	}
}
