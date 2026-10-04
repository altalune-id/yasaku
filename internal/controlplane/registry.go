package controlplane

import (
	"maps"
	"slices"

	apikeyv1connect "altalune.id/yasaku/gen/go/apikey/v1/apikeyv1connect"
	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	orgv1connect "altalune.id/yasaku/gen/go/org/v1/orgv1connect"
	projectv1connect "altalune.id/yasaku/gen/go/project/v1/projectv1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/platform/surfaces"
)

// VerbTable names the domain verb every mounted procedure exposes, keyed by procedure path.
func VerbTable() map[string]surfaces.Verb {
	return map[string]surfaces.Verb{
		apikeyv1connect.APIKeyServiceListProcedure:           {Module: "apikey", Aggregate: "apikey", Operation: "list"},
		apikeyv1connect.APIKeyServiceCreateProcedure:         {Module: "apikey", Aggregate: "apikey", Operation: "create"},
		apikeyv1connect.APIKeyServiceRevokeProcedure:         {Module: "apikey", Aggregate: "apikey", Operation: "revoke"},
		authv1connect.AuthServiceWhoamiProcedure:             {Module: "auth", Aggregate: "session", Operation: "whoami"},
		projectv1connect.ProjectServiceListProjectsProcedure: {Module: "project", Aggregate: "project", Operation: "list"},
		orgv1connect.MemberServiceListMembersProcedure:       {Module: "org", Aggregate: "member", Operation: "list"},

		yasakuv1connect.WorkspaceServiceListProjectsProcedure: {Module: "project", Aggregate: "project", Operation: "list_targets"},
		yasakuv1connect.WorkspaceServiceNowProcedure:          {Module: "ledger", Aggregate: "clock", Operation: "now"},

		yasakuv1connect.LedgerServiceGetSettingsProcedure:    {Module: "ledger", Aggregate: "settings", Operation: "get"},
		yasakuv1connect.LedgerServiceUpdateSettingsProcedure: {Module: "ledger", Aggregate: "settings", Operation: "update"},

		yasakuv1connect.WalletServiceListWalletsProcedure:     {Module: "wallet", Aggregate: "wallet", Operation: "list"},
		yasakuv1connect.WalletServiceGetWalletProcedure:       {Module: "wallet", Aggregate: "wallet", Operation: "get"},
		yasakuv1connect.WalletServiceWalletTotalsProcedure:    {Module: "wallet", Aggregate: "wallet", Operation: "totals"},
		yasakuv1connect.WalletServiceCreateWalletProcedure:    {Module: "wallet", Aggregate: "wallet", Operation: "create"},
		yasakuv1connect.WalletServiceUpdateWalletProcedure:    {Module: "wallet", Aggregate: "wallet", Operation: "update"},
		yasakuv1connect.WalletServiceArchiveWalletProcedure:   {Module: "wallet", Aggregate: "wallet", Operation: "archive"},
		yasakuv1connect.WalletServiceUnarchiveWalletProcedure: {Module: "wallet", Aggregate: "wallet", Operation: "unarchive"},
		yasakuv1connect.WalletServiceAdjustBalanceProcedure:   {Module: "wallet", Aggregate: "wallet", Operation: "adjust_balance"},
		yasakuv1connect.WalletServiceDeleteWalletProcedure:    {Module: "wallet", Aggregate: "wallet", Operation: "delete"},

		yasakuv1connect.CategoryServiceListCategoriesProcedure:        {Module: "category", Aggregate: "category", Operation: "list"},
		yasakuv1connect.CategoryServiceCreateCategoryProcedure:        {Module: "category", Aggregate: "category", Operation: "create"},
		yasakuv1connect.CategoryServiceSeedDefaultCategoriesProcedure: {Module: "category", Aggregate: "category", Operation: "seed_defaults"},
		yasakuv1connect.CategoryServiceRenameCategoryProcedure:        {Module: "category", Aggregate: "category", Operation: "rename"},
		yasakuv1connect.CategoryServiceUpdateCategoryProcedure:        {Module: "category", Aggregate: "category", Operation: "update"},
		yasakuv1connect.CategoryServiceArchiveCategoryProcedure:       {Module: "category", Aggregate: "category", Operation: "archive"},
		yasakuv1connect.CategoryServiceUnarchiveCategoryProcedure:     {Module: "category", Aggregate: "category", Operation: "unarchive"},
		yasakuv1connect.CategoryServiceDeleteCategoryProcedure:        {Module: "category", Aggregate: "category", Operation: "delete"},

		yasakuv1connect.TransactionServiceListTransactionsProcedure:   {Module: "transaction", Aggregate: "transaction", Operation: "list"},
		yasakuv1connect.TransactionServiceSearchTransactionsProcedure: {Module: "transaction", Aggregate: "transaction", Operation: "search"},
		yasakuv1connect.TransactionServiceSuggestCategoryProcedure:    {Module: "transaction", Aggregate: "transaction", Operation: "suggest_category"},
		yasakuv1connect.TransactionServiceRecordExpenseProcedure:      {Module: "transaction", Aggregate: "transaction", Operation: "record_expense"},
		yasakuv1connect.TransactionServiceRecordIncomeProcedure:       {Module: "transaction", Aggregate: "transaction", Operation: "record_income"},
		yasakuv1connect.TransactionServiceRecordTransferProcedure:     {Module: "transaction", Aggregate: "transaction", Operation: "record_transfer"},
		yasakuv1connect.TransactionServiceRecordBatchProcedure:        {Module: "transaction", Aggregate: "transaction", Operation: "record_batch"},
		yasakuv1connect.TransactionServiceReviseTransactionProcedure:  {Module: "transaction", Aggregate: "transaction", Operation: "revise"},
		yasakuv1connect.TransactionServiceDeleteTransactionProcedure:  {Module: "transaction", Aggregate: "transaction", Operation: "delete"},

		yasakuv1connect.PeriodServiceGetCurrentPeriodProcedure: {Module: "period", Aggregate: "period", Operation: "get_current"},
		yasakuv1connect.PeriodServiceListPeriodsProcedure:      {Module: "period", Aggregate: "period", Operation: "list"},
		yasakuv1connect.PeriodServicePreviewCloseProcedure:     {Module: "period", Aggregate: "period", Operation: "preview_close"},
		yasakuv1connect.PeriodServiceClosePeriodProcedure:      {Module: "period", Aggregate: "period", Operation: "close"},
		yasakuv1connect.PeriodServiceRenamePeriodProcedure:     {Module: "period", Aggregate: "period", Operation: "rename"},
		yasakuv1connect.PeriodServiceReopenPeriodProcedure:     {Module: "period", Aggregate: "period", Operation: "reopen"},

		yasakuv1connect.ReportServicePeriodReportProcedure:   {Module: "report", Aggregate: "report", Operation: "period"},
		yasakuv1connect.ReportServiceCashflowReportProcedure: {Module: "report", Aggregate: "report", Operation: "cashflow"},

		yasakuv1connect.OpensheetServiceGetOpensheetLinkProcedure:        {Module: "opensheetsync", Aggregate: "link", Operation: "get"},
		yasakuv1connect.OpensheetServiceTestOpensheetLinkProcedure:       {Module: "opensheetsync", Aggregate: "link", Operation: "test"},
		yasakuv1connect.OpensheetServiceSaveOpensheetLinkProcedure:       {Module: "opensheetsync", Aggregate: "link", Operation: "save"},
		yasakuv1connect.OpensheetServiceSetOpensheetLinkEnabledProcedure: {Module: "opensheetsync", Aggregate: "link", Operation: "set_enabled"},
		yasakuv1connect.OpensheetServiceSyncOpensheetNowProcedure:        {Module: "opensheetsync", Aggregate: "link", Operation: "sync_now"},
		yasakuv1connect.OpensheetServiceDeleteOpensheetLinkProcedure:     {Module: "opensheetsync", Aggregate: "link", Operation: "delete"},
	}
}

// Verbs returns the domain verbs S2 exposes, deduplicated.
func Verbs() []surfaces.Verb {
	return slices.Collect(maps.Values(VerbTable()))
}
