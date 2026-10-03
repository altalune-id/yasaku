package boot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	rootmcp "altalune.id/yasaku/mcp"
)

func decodeFailure(t *testing.T, input string) error {
	t.Helper()
	err := protojson.Unmarshal([]byte(input), &apperrorv1.ErrorDetail{})
	require.Error(t, err, "protojson accepted %s", input)
	return err
}

func TestMCPErrorPayloadMapsUndecodableArgumentsToAValidationError(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
	}{
		{name: "a wrong JSON type", input: `{"code":{}}`, meta: map[string]string{"tool": "blog_publish", "field": "code"}},
		{name: "an unknown field", input: `{"bogus":1}`, meta: map[string]string{"tool": "blog_publish", "field": "bogus"}},
		{name: "a string where an object belongs", input: `{"meta":"x"}`, meta: map[string]string{"tool": "blog_publish"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cause := rootmcp.NewInvalidArgumentsError("blog_publish", decodeFailure(t, tc.input))

			payload := mcpErrorPayload(t.Context(), cause)

			require.Equal(t, apperror.CodeValidation, payload.Code)
			require.Equal(t, tc.meta, payload.Meta)
			require.Contains(t, payload.Message, cause.Reason)
		})
	}
}
