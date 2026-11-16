package boot

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBootServer_BrokenThemeSourceStopsStartup(t *testing.T) {
	broken := errors.New("theme source broken")
	orig := loadThemes
	loadThemes = func() error { return broken }
	t.Cleanup(func() { loadThemes = orig })

	srv, err := BootServer(t.Context(), queueBootCfg(t, ""))
	if srv != nil {
		t.Cleanup(func() { _ = srv.Close() })
	}
	require.ErrorIs(t, err, broken)
	require.Nil(t, srv)
}
