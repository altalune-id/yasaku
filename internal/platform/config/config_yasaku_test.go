package config

import "testing"

func TestOpensheet_UnmountedByDefault(t *testing.T) {
	if Defaults().Opensheet.Mounted() {
		t.Fatal("the opensheet module must be unmounted unless opensheet.baseURL is set")
	}
}

func TestLoad_OpensheetFromEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YASAKU_OPENSHEET_BASE_URL", "https://sheets.example.com")
	t.Setenv("YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS", "true")
	cfg, err := Load("", withCwdOverride(t, t.TempDir()), withGenesisFallback(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Opensheet.BaseURL != "https://sheets.example.com" || !cfg.Opensheet.AllowPrivateHosts || !cfg.Opensheet.Mounted() {
		t.Fatalf("opensheet = %+v", cfg.Opensheet)
	}
}

func TestLoad_OpensheetBaseURLMustBeAURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YASAKU_OPENSHEET_BASE_URL", "not a url")
	if _, err := Load("", withCwdOverride(t, t.TempDir()), withGenesisFallback(t)); err == nil {
		t.Fatal("a non-URL opensheet.baseURL must fail validation")
	}
}

func TestOpensheet_BlankBaseURLIsUnmounted(t *testing.T) {
	if (OpensheetConfig{BaseURL: "   "}).Mounted() {
		t.Fatal("a blank base URL must not mount the module")
	}
}
