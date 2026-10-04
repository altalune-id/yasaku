package boot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/scheduler"
)

func yasakuSchedulerStubs() []scheduler.Provider {
	out := make([]scheduler.Provider, len(yasakuSchedulerDomains()))
	for i := range out {
		out[i] = stubProvider{}
	}
	return out
}

func TestAssertSchedulerWiring_NamesAMissingYasakuSlot(t *testing.T) {
	template := len(schedulerDomains) - len(yasakuSchedulerDomains())
	for i, domain := range yasakuSchedulerDomains() {
		t.Run(domain, func(t *testing.T) {
			ps := yasakuSchedulerStubs()
			for range template {
				ps = append([]scheduler.Provider{stubProvider{}}, ps...)
			}
			ps[template+i] = nil
			require.ErrorContains(t, assertSchedulerWiring(ps), domain)
		})
	}
}

func TestYasakuSchedulerProviders_MatchTheirDomains(t *testing.T) {
	require.Len(t, yasakuSchedulerProviders(&Services{}, discardLogger()), len(yasakuSchedulerDomains()))
	require.Contains(t, yasakuSchedulerDomains(), "opensheetsync")
}
