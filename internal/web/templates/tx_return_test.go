package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTxReturn_OneAllowlistForPathAndPost(t *testing.T) {
	t.Parallel()
	const id = "0b6f7c1e-4a8e-4f61-9b0a-1f5c2d3e4a5b"
	base := "/orgs/acme/projects/home"
	cases := []struct{ path, want string }{
		{base + "/overview", "overview"},
		{base + "/transactions?period=x", "transactions"},
		{base + "/wallets/" + id, "wallets/" + id},
		{base + "/wallets/not-a-uuid", ""},
		{base + "/wallets/" + id + "/edit", ""},
		{base + "/settings", ""},
	}
	for _, tc := range cases {
		got := TxReturnFor(tc.path)
		assert.Equal(t, tc.want, got, tc.path)
		assert.Equal(t, got, TxReturnPage(got), "the handler must accept every value the template emits: %s", tc.path)
	}
	assert.Empty(t, TxReturnPage("https://evil.example"))
	assert.Empty(t, TxReturnPage("../overview"))
}
