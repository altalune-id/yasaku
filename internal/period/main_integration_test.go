//go:build integration

package period_test

import (
	"testing"

	"altalune.id/yasaku/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}
