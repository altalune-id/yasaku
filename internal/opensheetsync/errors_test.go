package opensheetsync_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/sealer"
)

func TestErrors_CodeAndHelper(t *testing.T) {
	tests := []struct {
		err  error
		code string
		grpc codes.Code
		is   func(error) bool
	}{
		{&opensheetsync.LinkNotFoundError{ProjectID: "p"}, apperror.CodeOpensheetLinkNotFound, codes.NotFound, opensheetsync.IsLinkNotFoundError},
		{&opensheetsync.InvalidSettingError{Field: "os_org", Reason: "r"}, apperror.CodeOpensheetInvalidSetting, codes.InvalidArgument, opensheetsync.IsInvalidSettingError},
		{&opensheetsync.APIKeyRequiredError{}, apperror.CodeOpensheetAPIKeyRequired, codes.InvalidArgument, opensheetsync.IsAPIKeyRequiredError},
		{&opensheetsync.SheetUnreachableError{Sheet: "s"}, apperror.CodeOpensheetSheetUnreachable, codes.FailedPrecondition, opensheetsync.IsSheetUnreachableError},
		{&opensheetsync.ShapeMismatchError{Sheet: "s", Missing: []string{"id"}}, apperror.CodeOpensheetShapeMismatch, codes.FailedPrecondition, opensheetsync.IsShapeMismatchError},
		{&opensheetsync.NoIDColumnError{Sheet: "s"}, apperror.CodeOpensheetNoIDColumn, codes.FailedPrecondition, opensheetsync.IsNoIDColumnError},
		{&opensheetsync.SheetNotWritableError{Sheet: "s"}, apperror.CodeOpensheetSheetNotWritable, codes.FailedPrecondition, opensheetsync.IsSheetNotWritableError},
		{&opensheetsync.ContractUnsatisfiedError{Sheet: "s", Reason: "r"}, apperror.CodeOpensheetContractUnsatisfied, codes.FailedPrecondition, opensheetsync.IsContractUnsatisfiedError},
		{&opensheetsync.UnavailableError{Sheet: "s", Cause: "c"}, apperror.CodeOpensheetUnavailable, codes.Unavailable, opensheetsync.IsUnavailableError},
		{&opensheetsync.NotVerifiedError{}, apperror.CodeOpensheetNotVerified, codes.FailedPrecondition, opensheetsync.IsNotVerifiedError},
		{&opensheetsync.LinkDisabledError{}, apperror.CodeOpensheetLinkDisabled, codes.FailedPrecondition, opensheetsync.IsLinkDisabledError},
		{&opensheetsync.SyncRefusedError{Sheet: "s", Code: "SHT009", Cause: errors.New("x")}, apperror.CodeOpensheetSyncRefused, codes.FailedPrecondition, opensheetsync.IsSyncRefusedError},
		{&opensheetsync.ScopeMismatchError{Want: "a", Got: "b"}, apperror.CodeOpensheetScopeMismatch, codes.FailedPrecondition, opensheetsync.IsScopeMismatchError},
		{&opensheetsync.RowRefusedError{Sheet: "s", Code: "SHT014", Cause: errors.New("x")}, apperror.CodeOpensheetRowRefused, codes.FailedPrecondition, opensheetsync.IsRowRefusedError},
		{&opensheetsync.KeyUnreadableError{Cause: errors.New("x")}, apperror.CodeOpensheetKeyUnreadable, codes.FailedPrecondition, opensheetsync.IsKeyUnreadableError},
		{&opensheetsync.PrivateEndpointError{Cause: errors.New("x")}, apperror.CodeOpensheetPrivateEndpoint, codes.FailedPrecondition, opensheetsync.IsPrivateEndpointError},
	}
	for _, tt := range tests {
		ae, ok := apperror.AsAppError(tt.err)
		require.True(t, ok, "%T", tt.err)
		require.Equal(t, tt.code, ae.Code(), "%T", tt.err)
		require.Equal(t, tt.grpc, ae.GRPCCode(), "%T", tt.err)
		require.True(t, tt.is(fmt.Errorf("wrapped: %w", tt.err)), "%T helper must walk %%w", tt.err)
		require.Contains(t, tt.err.Error(), "opensheetsync: ")
	}
}

func TestErrors_ANilCauseNeverPanics(t *testing.T) {
	for _, err := range []error{
		&opensheetsync.SyncRefusedError{Sheet: "s", Code: "SHT001"},
		&opensheetsync.RowRefusedError{Sheet: "s", Code: "SHT023"},
	} {
		require.NotPanics(t, func() { _ = err.Error() }, "%T", err)
		require.Contains(t, err.Error(), "sheet s")
		require.NoError(t, errors.Unwrap(err))
	}
}

func TestKeyUnreadableError_AsksForTheKeyAgainAndKeepsItsCause(t *testing.T) {
	err := &opensheetsync.KeyUnreadableError{Cause: &sealer.OpenFailedError{}}
	ae, ok := apperror.AsAppError(fmt.Errorf("wrapped: %w", err))
	require.True(t, ok)
	require.Equal(t, apperror.CodeOpensheetKeyUnreadable, ae.Code(), "the sealer's own code never leaks through the service")
	require.Equal(t, "The saved opensheet API key can no longer be read; enter it again and save", ae.Message())
	require.True(t, sealer.IsOpenFailedError(err))
	require.NotPanics(t, func() { _ = (&opensheetsync.KeyUnreadableError{}).Error() })
}

func TestShapeMismatchError_TellsThePersonOpensheetMayHaveTheOldHeader(t *testing.T) {
	ae, ok := apperror.AsAppError(&opensheetsync.ShapeMismatchError{Sheet: "s", Missing: []string{"note", "period"}})
	require.True(t, ok)
	require.Contains(t, ae.Message(), "note, period")
	require.Contains(t, ae.Message(), "opensheet may still have the old header; open the tab's rows in opensheet (or wait a few minutes) and run Test again")
}
