package boot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/api"

	mcprt "altalune.id/yasaku/mcp"
)

func TestAssertMCPWiring(t *testing.T) {
	full := make([]func(mcprt.Registry), len(mcpDomains))
	for i := range full {
		full[i] = func(mcprt.Registry) {}
	}

	tests := []struct {
		name       string
		registrars []func(mcprt.Registry)
		wantErr    string
	}{
		{name: "complete list passes", registrars: full},
		{name: "empty list fails", registrars: nil, wantErr: "0 registrars"},
		{name: "short list fails", registrars: full[:len(full)-1], wantErr: "registrars for"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertMCPWiring(tc.registrars)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestAssertMCPWiring_RejectsANilSlot(t *testing.T) {
	registrars := make([]func(mcprt.Registry), len(mcpDomains))
	for i := range registrars {
		registrars[i] = func(mcprt.Registry) {}
	}
	registrars[2] = nil

	require.ErrorContains(t, assertMCPWiring(registrars), mcpDomains[2])
}

func TestMCPRegistrars_CoverEveryManifestDomain(t *testing.T) {
	require.NoError(t, assertMCPWiring(mcpRegistrars(&api.Server{})))
}
