package gen

import (
	"strings"
	"testing"
)

func TestOptionsSet(t *testing.T) {
	tests := []struct {
		name, key, value, wantPrefix, wantErr string
	}{
		{name: "valid", key: "ui_prefix", value: "ui://yasaku", wantPrefix: "ui://yasaku"},
		{name: "unknown key", key: "nope", value: "x", wantErr: `unknown parameter "nope"`},
		{name: "bad scheme", key: "ui_prefix", value: "https://yasaku", wantErr: "scheme must be ui"},
		{name: "not a uri", key: "ui_prefix", value: "::::", wantErr: "ui_prefix"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var o Options
			err := o.Set(tc.key, tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Set() = %v, want nil", err)
				}
				if o.UIPrefix != tc.wantPrefix {
					t.Errorf("UIPrefix = %q, want %q", o.UIPrefix, tc.wantPrefix)
				}
				return
			}
			if err == nil {
				t.Fatalf("Set() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Set() = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
