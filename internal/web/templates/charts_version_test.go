package templates

import (
	"os"
	"regexp"
	"testing"
)

// TestEChartsVersionMatchesVendorScript keeps the CDN and vendored modes on one ECharts build.
func TestEChartsVersionMatchesVendorScript(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("../../../scripts/ui-vendor.sh")
	if err != nil {
		t.Fatalf("read ui-vendor.sh: %v", err)
	}
	m := regexp.MustCompile(`ECHARTS_VERSION="\$\{ECHARTS_VERSION:-([^}"]+)\}"`).FindSubmatch(b)
	if m == nil {
		t.Fatal("ECHARTS_VERSION default not found in scripts/ui-vendor.sh — the pattern has gone stale")
	}
	if got := string(m[1]); got != echartsVersion {
		t.Fatalf("ui-vendor.sh pins echarts %s, charts.templ pins %s", got, echartsVersion)
	}
}
