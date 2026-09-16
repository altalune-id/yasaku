package api

import "testing"

// The contract's cashflow numbers have no domain constant behind them, and an end-to-end test
// cannot tell 6 from 24 without seeding 25 periods, so the clamp is asserted directly.
func TestClampCashflowPeriods(t *testing.T) {
	cases := map[string]struct {
		requested int32
		want      int
	}{
		"unset defaults to six":    {0, 6},
		"negative defaults to six": {-3, 6},
		"one is honoured":          {1, 1},
		"five is honoured":         {5, 5},
		"six is honoured":          {6, 6},
		"seven is honoured":        {7, 7},
		"twenty-four is the cap":   {24, 24},
		"twenty-five is capped":    {25, 24},
		"a thousand is capped":     {1000, 24},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := clampCashflowPeriods(tc.requested); got != tc.want {
				t.Errorf("clampCashflowPeriods(%d) = %d, want %d", tc.requested, got, tc.want)
			}
		})
	}
}
