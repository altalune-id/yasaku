package controlplane

import (
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/platform/authn"
)

func yasakuProcedureScopes() authn.ScopeTable {
	r, w := authn.ScopeYasakuRead, authn.ScopeYasakuWrite
	return authn.ScopeTable{
		yasakuv1connect.WorkspaceServiceListProjectsProcedure: r,
		yasakuv1connect.WorkspaceServiceNowProcedure:          r,

		yasakuv1connect.LedgerServiceGetSettingsProcedure:    r,
		yasakuv1connect.LedgerServiceUpdateSettingsProcedure: w,

		yasakuv1connect.WalletServiceListWalletsProcedure:     r,
		yasakuv1connect.WalletServiceGetWalletProcedure:       r,
		yasakuv1connect.WalletServiceWalletTotalsProcedure:    r,
		yasakuv1connect.WalletServiceCreateWalletProcedure:    w,
		yasakuv1connect.WalletServiceUpdateWalletProcedure:    w,
		yasakuv1connect.WalletServiceArchiveWalletProcedure:   w,
		yasakuv1connect.WalletServiceUnarchiveWalletProcedure: w,
		yasakuv1connect.WalletServiceAdjustBalanceProcedure:   w,
		yasakuv1connect.WalletServiceDeleteWalletProcedure:    w,

		yasakuv1connect.CategoryServiceListCategoriesProcedure:        r,
		yasakuv1connect.CategoryServiceCreateCategoryProcedure:        w,
		yasakuv1connect.CategoryServiceSeedDefaultCategoriesProcedure: w,
		yasakuv1connect.CategoryServiceRenameCategoryProcedure:        w,
		yasakuv1connect.CategoryServiceUpdateCategoryProcedure:        w,
		yasakuv1connect.CategoryServiceArchiveCategoryProcedure:       w,
		yasakuv1connect.CategoryServiceUnarchiveCategoryProcedure:     w,
		yasakuv1connect.CategoryServiceDeleteCategoryProcedure:        w,

		yasakuv1connect.TransactionServiceListTransactionsProcedure:   r,
		yasakuv1connect.TransactionServiceSearchTransactionsProcedure: r,
		yasakuv1connect.TransactionServiceSuggestCategoryProcedure:    r,
		yasakuv1connect.TransactionServiceRecordExpenseProcedure:      w,
		yasakuv1connect.TransactionServiceRecordIncomeProcedure:       w,
		yasakuv1connect.TransactionServiceRecordTransferProcedure:     w,
		yasakuv1connect.TransactionServiceRecordBatchProcedure:        w,
		yasakuv1connect.TransactionServiceReviseTransactionProcedure:  w,
		yasakuv1connect.TransactionServiceDeleteTransactionProcedure:  w,

		yasakuv1connect.PeriodServiceGetCurrentPeriodProcedure: r,
		yasakuv1connect.PeriodServiceListPeriodsProcedure:      r,
		yasakuv1connect.PeriodServicePreviewCloseProcedure:     r,
		yasakuv1connect.PeriodServiceClosePeriodProcedure:      w,
		yasakuv1connect.PeriodServiceRenamePeriodProcedure:     w,
		yasakuv1connect.PeriodServiceReopenPeriodProcedure:     w,

		yasakuv1connect.ReportServicePeriodReportProcedure:   r,
		yasakuv1connect.ReportServiceCashflowReportProcedure: r,

		yasakuv1connect.OpensheetServiceGetOpensheetLinkProcedure:        r,
		yasakuv1connect.OpensheetServiceTestOpensheetLinkProcedure:       w,
		yasakuv1connect.OpensheetServiceSaveOpensheetLinkProcedure:       w,
		yasakuv1connect.OpensheetServiceSetOpensheetLinkEnabledProcedure: w,
		yasakuv1connect.OpensheetServiceSyncOpensheetNowProcedure:        w,
		yasakuv1connect.OpensheetServiceDeleteOpensheetLinkProcedure:     w,
	}
}

// YasakuFineProcedureScopes is the target scope per procedure once yasaku:* is retired. TODO(scopes): enforce this table instead of yasakuProcedureScopes, add its scopes to the authn catalogue and authl, then remove yasaku:*.
func YasakuFineProcedureScopes() authn.ScopeTable {
	return authn.ScopeTable{
		yasakuv1connect.WorkspaceServiceListProjectsProcedure: "ledgers:read",
		yasakuv1connect.WorkspaceServiceNowProcedure:          "ledgers:read",

		yasakuv1connect.LedgerServiceGetSettingsProcedure:    "ledgers:read",
		yasakuv1connect.LedgerServiceUpdateSettingsProcedure: "ledgers:write",

		yasakuv1connect.WalletServiceListWalletsProcedure:     "wallets:read",
		yasakuv1connect.WalletServiceGetWalletProcedure:       "wallets:read",
		yasakuv1connect.WalletServiceWalletTotalsProcedure:    "wallets:read",
		yasakuv1connect.WalletServiceCreateWalletProcedure:    "wallets:write",
		yasakuv1connect.WalletServiceUpdateWalletProcedure:    "wallets:write",
		yasakuv1connect.WalletServiceArchiveWalletProcedure:   "wallets:write",
		yasakuv1connect.WalletServiceUnarchiveWalletProcedure: "wallets:write",
		yasakuv1connect.WalletServiceAdjustBalanceProcedure:   "wallets:write",
		yasakuv1connect.WalletServiceDeleteWalletProcedure:    "wallets:admin",

		yasakuv1connect.CategoryServiceListCategoriesProcedure:        "categories:read",
		yasakuv1connect.CategoryServiceCreateCategoryProcedure:        "categories:write",
		yasakuv1connect.CategoryServiceSeedDefaultCategoriesProcedure: "categories:write",
		yasakuv1connect.CategoryServiceRenameCategoryProcedure:        "categories:write",
		yasakuv1connect.CategoryServiceUpdateCategoryProcedure:        "categories:write",
		yasakuv1connect.CategoryServiceArchiveCategoryProcedure:       "categories:write",
		yasakuv1connect.CategoryServiceUnarchiveCategoryProcedure:     "categories:write",
		yasakuv1connect.CategoryServiceDeleteCategoryProcedure:        "categories:admin",

		yasakuv1connect.TransactionServiceListTransactionsProcedure:   "transactions:read",
		yasakuv1connect.TransactionServiceSearchTransactionsProcedure: "transactions:read",
		yasakuv1connect.TransactionServiceSuggestCategoryProcedure:    "transactions:read",
		yasakuv1connect.TransactionServiceRecordExpenseProcedure:      "transactions:write",
		yasakuv1connect.TransactionServiceRecordIncomeProcedure:       "transactions:write",
		yasakuv1connect.TransactionServiceRecordTransferProcedure:     "transactions:write",
		yasakuv1connect.TransactionServiceRecordBatchProcedure:        "transactions:write",
		yasakuv1connect.TransactionServiceReviseTransactionProcedure:  "transactions:write",
		yasakuv1connect.TransactionServiceDeleteTransactionProcedure:  "transactions:admin",

		yasakuv1connect.PeriodServiceGetCurrentPeriodProcedure: "periods:read",
		yasakuv1connect.PeriodServiceListPeriodsProcedure:      "periods:read",
		yasakuv1connect.PeriodServicePreviewCloseProcedure:     "periods:read",
		yasakuv1connect.PeriodServiceClosePeriodProcedure:      "periods:write",
		yasakuv1connect.PeriodServiceRenamePeriodProcedure:     "periods:write",
		yasakuv1connect.PeriodServiceReopenPeriodProcedure:     "periods:admin",

		yasakuv1connect.ReportServicePeriodReportProcedure:   "reports:read",
		yasakuv1connect.ReportServiceCashflowReportProcedure: "reports:read",

		yasakuv1connect.OpensheetServiceGetOpensheetLinkProcedure:        "opensheet:read",
		yasakuv1connect.OpensheetServiceTestOpensheetLinkProcedure:       "opensheet:write",
		yasakuv1connect.OpensheetServiceSaveOpensheetLinkProcedure:       "opensheet:write",
		yasakuv1connect.OpensheetServiceSetOpensheetLinkEnabledProcedure: "opensheet:write",
		yasakuv1connect.OpensheetServiceSyncOpensheetNowProcedure:        "opensheet:write",
		yasakuv1connect.OpensheetServiceDeleteOpensheetLinkProcedure:     "opensheet:write",
	}
}
