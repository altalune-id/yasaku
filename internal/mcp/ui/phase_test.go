package ui

import "testing"

func TestPhaseOf(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct{ name, payload, want string }{
		{"needs", `{needs:{needs:[{field:"wallet",reason:"which one?"}]}}`, "needs"},
		{"needs wins over preview", `{needs:{needs:[{field:"w"}]},preview:{id:"x"}}`, "needs"},
		{"empty needs wrapper is not needs", `{needs:{},preview:{id:"x"}}`, "preview"},
		{"preview", `{preview:{id:"x"}}`, "preview"},
		{"result", `{result:{id:"x"}}`, "result"},
		{"adjust_balance no-op", `{warning:"nothing was written"}`, "empty"},
		{"seed zero", `{}`, "empty"},
		{"batch preview", `{preview:[{transaction:{id:"a"}}]}`, "preview"},
		{"batch result", `{results:[{transaction:{id:"a"}}]}`, "result"},
		{"seed preview", `{previewCount:12}`, "preview"},
		{"seed result", `{inserted:12}`, "result"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsString(t, vm, "phaseOf("+tc.payload+")"); got != tc.want {
				t.Errorf("phaseOf(%s) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestNeedsListReadsTheDoubleWrapper(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `JSON.stringify(needsList({needs:{needs:[{field:"org",reason:"pick",candidates:["a","b"]}]}}))`)
	const want = `[{"field":"org","reason":"pick","candidates":["a","b"]}]`
	if got != want {
		t.Errorf("needsList = %s, want %s", got, want)
	}
	for _, expr := range []string{`needsList({})`, `needsList({needs:{}})`, `needsList(undefined)`} {
		if got := jsString(t, vm, "JSON.stringify("+expr+")"); got != "[]" {
			t.Errorf("%s = %s, want []", expr, got)
		}
	}
}
