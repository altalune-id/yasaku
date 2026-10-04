package opensheetsync_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
)

func goodSettings() opensheetsync.Settings {
	return opensheetsync.Settings{OSOrg: "acme", OSProject: "home", APIKey: "osk_live_0123456789abcd", Sheets: opensheetsync.DefaultSheetSlugs()}
}

func TestSettings_Validate(t *testing.T) {
	tests := []struct {
		name       string
		edit       func(*opensheetsync.Settings)
		requireKey bool
		is         func(error) bool
	}{
		{"valid", func(*opensheetsync.Settings) {}, true, nil},
		{"upper-case org", func(s *opensheetsync.Settings) { s.OSOrg = "Acme" }, true, opensheetsync.IsInvalidSettingError},
		{"empty project", func(s *opensheetsync.Settings) { s.OSProject = "" }, true, opensheetsync.IsInvalidSettingError},
		{"one sheet twice", func(s *opensheetsync.Settings) { s.Sheets.Wallets = s.Sheets.Transactions }, true, opensheetsync.IsInvalidSettingError},
		{"slug too long", func(s *opensheetsync.Settings) { s.Sheets.Categories = strings.Repeat("a", 65) }, true, opensheetsync.IsInvalidSettingError},
		{"no key on first save", func(s *opensheetsync.Settings) { s.APIKey = "" }, true, opensheetsync.IsAPIKeyRequiredError},
		{"no key keeps the saved one", func(s *opensheetsync.Settings) { s.APIKey = "" }, false, nil},
		{"key with a space", func(s *opensheetsync.Settings) { s.APIKey = "osk live" }, true, opensheetsync.IsInvalidSettingError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := goodSettings()
			tt.edit(&s)
			err := s.Validate(tt.requireKey)
			if tt.is == nil {
				require.NoError(t, err)
				return
			}
			require.True(t, tt.is(err), "got %v", err)
		})
	}
}

func TestSettings_NormalizedTrims(t *testing.T) {
	s := opensheetsync.Settings{OSOrg: " acme ", OSProject: "home\n", APIKey: " k ", Sheets: opensheetsync.SheetSlugs{Transactions: " t "}}.Normalized()
	require.Equal(t, "acme", s.OSOrg)
	require.Equal(t, "home", s.OSProject)
	require.Equal(t, "k", s.APIKey)
	require.Equal(t, "t", s.Sheets.Transactions)
}

func TestKeyHint_ShowsOnlyTheLastFourOfALongKey(t *testing.T) {
	require.Equal(t, "abcd", opensheetsync.KeyHint("osk_live_0123456789abcd"))
	require.Equal(t, "", opensheetsync.KeyHint("short"), "a short key shows nothing rather than most of itself")
}

func TestLink_ConfigureEnableDisable(t *testing.T) {
	at := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	l := opensheetsync.NewLink(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, at)
	require.Equal(t, opensheetsync.DefaultSheetSlugs(), l.Sheets)
	require.True(t, opensheetsync.IsNotVerifiedError(l.Enable(at)), "an untested link cannot be enabled")

	l.Configure(goodSettings(), []byte("sealed"), "abcd", at)
	require.Equal(t, "acme", l.OSOrg)
	require.Equal(t, []byte("sealed"), l.APIKeySealed)
	require.NotNil(t, l.VerifiedAt)

	l.Configure(goodSettings(), nil, "", at.Add(time.Hour))
	require.Equal(t, []byte("sealed"), l.APIKeySealed, "a nil sealed key keeps the saved one")
	require.Equal(t, "abcd", l.APIKeyHint)

	l.FailureStreak, l.LastError = 2, "SHT014"
	tripped := at.Add(90 * time.Minute)
	l.AutoDisabledAt = &tripped
	l.Configure(goodSettings(), nil, "", at.Add(2*time.Hour))
	require.Zero(t, l.FailureStreak, "new verified settings start a fresh streak")
	require.False(t, l.AutoDisabled(), "a passing Save answers the auto-disable; the link is simply off")
	require.Equal(t, "SHT014", l.LastError, "the last sync error stays visible next to the Test")

	l.FailureStreak, l.LastError = 2, "boom"
	auto := at
	l.AutoDisabledAt = &auto
	require.NoError(t, l.Enable(at))
	require.True(t, l.Enabled)
	require.Zero(t, l.FailureStreak)
	require.Empty(t, l.LastError)
	require.False(t, l.AutoDisabled())

	l.Disable(at)
	require.False(t, l.Enabled)

	moved := goodSettings()
	require.False(t, l.Retargets(moved))
	moved.Sheets.Wallets = "other"
	require.True(t, l.Retargets(moved))
	require.Empty(t, l.Settings().APIKey, "Settings never carries the key back")
}

func TestSettings_NeverPrintTheKey(t *testing.T) {
	s := goodSettings()
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), s.String()} {
		require.NotContains(t, out, s.APIKey)
		require.Contains(t, out, "[redacted]")
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("save", "settings", s)
	require.NotContains(t, buf.String(), s.APIKey)
	require.Contains(t, buf.String(), `"os_org":"acme"`)
}

func TestLink_AwaitingFirstSyncUntilASyncLandsAfterTheLastSave(t *testing.T) {
	at := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	l := opensheetsync.NewLink(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, at)
	require.False(t, l.AwaitingFirstSync(), "an unsaved link waits for a Save, not a sync")
	l.Configure(goodSettings(), []byte("sealed"), "abcd", at)
	require.True(t, l.AwaitingFirstSync())
	synced := at.Add(time.Minute)
	l.LastSyncedAt = &synced
	require.False(t, l.AwaitingFirstSync())
	l.Configure(goodSettings(), nil, "", at.Add(time.Hour))
	require.True(t, l.AwaitingFirstSync(), "a re-save may point at other sheets, so the next sync confirms them again")
}
