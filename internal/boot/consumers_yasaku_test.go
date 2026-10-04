package boot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/queue"
)

func yasakuConsumerStubs() []queue.Provider {
	out := make([]queue.Provider, len(yasakuConsumerDomains()))
	for i := range out {
		out[i] = stubConsumerProvider{}
	}
	return out
}

func TestAssertConsumerWiring_NamesAMissingYasakuSlot(t *testing.T) {
	template := len(consumerDomains()) - len(yasakuConsumerDomains())
	for i, domain := range yasakuConsumerDomains() {
		t.Run(domain, func(t *testing.T) {
			ps := yasakuConsumerStubs()
			for range template {
				ps = append([]queue.Provider{stubConsumerProvider{}}, ps...)
			}
			ps[template+i] = nil
			require.ErrorContains(t, assertConsumerWiring(ps), domain)
		})
	}
}

func TestYasakuConsumerProviders_MatchTheirDomains(t *testing.T) {
	require.Len(t, yasakuConsumerProviders(&Services{}, discardLogger()), len(yasakuConsumerDomains()))
	require.Contains(t, yasakuConsumerDomains(), "opensheetsync")
}
