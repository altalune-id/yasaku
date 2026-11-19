package opensheetsync_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/opensheet"
)

func TestErrorCode_ReadsTheCodeBackFromEveryStoredRefusal(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"key refused with opensheet's message", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Sheet: "tx", Code: "SHT009",
			Cause: &opensheet.APIError{Status: 403, Code: "SHT009", Message: "key lacks sheets:write OSL999"}}), "SHT009"},
		{"sheet missing without a message", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Sheet: "tx", Code: "SHT008",
			Cause: &opensheet.NotFoundError{Slug: "tx", Code: "SHT008"}}), "SHT008"},
		{"expired Google sign-in", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Sheet: "tx", Code: "CRD006",
			Cause: &opensheet.APIError{Status: 424, Code: "CRD006", Message: "credential expired"}}), "CRD006"},
		{"unreadable key", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Code: "key",
			Cause: &opensheetsync.KeyUnreadableError{Cause: errors.New("cipher: message authentication failed")}}), "OSL015"},
		{"private endpoint", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Code: "config",
			Cause: &opensheetsync.PrivateEndpointError{Cause: errors.New("dial 10.0.0.1")}}), "OSL016"},
		{"a config refusal carries no code", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Code: "config", Cause: errors.New("bad url")}), ""},
		{"row refused", opensheetsync.FailureText(&opensheetsync.RowRefusedError{Sheet: "tx", Code: "SHT016",
			Cause: &opensheet.ValidationError{Code: "SHT016", Message: "amount must be a number"}}), "SHT016"},
		{"a detail quoting another code keeps the real one", opensheetsync.RefusalText(&opensheetsync.SyncRefusedError{Sheet: "tx", Code: "SHT008",
			Cause: &opensheet.APIError{Status: 404, Code: "SHT008", Message: "tab gone (GSH002)"}}), "SHT008"},
		{"no code but a detail quoting one reads none", opensheetsync.FailureText(&opensheetsync.RowRefusedError{Sheet: "tx",
			Cause: &opensheet.APIError{Status: 400, Message: "value (ABC123) rejected"}}), ""},
		{"a code in a trailing parenthesis", "opensheet refused the sync (SHT001)", "SHT001"},
		{"no text", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, opensheetsync.ErrorCode(tt.text), "text %q", tt.text)
		})
	}
}

func TestReasonOf_NamesWhatEachOpensheetCodeMeans(t *testing.T) {
	tests := map[string]opensheetsync.Reason{
		"GEN002": opensheetsync.ReasonKeyRejected, "AUT001": opensheetsync.ReasonKeyRejected, "GEN003": opensheetsync.ReasonKeyRejected,
		"SHT009": opensheetsync.ReasonSheetReadOnly,
		"SHT001": opensheetsync.ReasonSheetMissing, "SHT008": opensheetsync.ReasonSheetMissing,
		"CRD006": opensheetsync.ReasonGoogleAccess, "GSH002": opensheetsync.ReasonGoogleAccess,
		"OSL015": opensheetsync.ReasonKeyUnreadable, "OSL016": opensheetsync.ReasonPrivateEndpoint,
		"SHT010": opensheetsync.ReasonOther, "": opensheetsync.ReasonOther,
	}
	for code, want := range tests {
		assert.Equal(t, want, opensheetsync.ReasonOf(code), code)
	}
}
