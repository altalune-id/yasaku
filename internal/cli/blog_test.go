package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordingServer struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
	body  string
}

func newRecordingServer(t *testing.T, status int, body string) *recordingServer {
	t.Helper()
	rs := &recordingServer{body: body}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.auths = append(rs.auths, r.Header.Get("Authorization"))
		rs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(rs.body))
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) sawCredential(key string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, a := range rs.auths {
		if strings.Contains(a, key) {
			return true
		}
	}
	return false
}

func (rs *recordingServer) hits() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.auths)
}

func runBlog(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd(stubServerBoot, stubClientBoot)
	if c, _, err := root.Find([]string{"blog"}); err != nil || c == root {
		root.AddCommand(newBlogCmd())
	}
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestBlogList_RendersJSON(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK,
		`{"posts":[{"id":"p1","slug":"hello","title":"Hello","status":"published","version":2}]}`)

	out, err := runBlog(t, "blog", "list",
		"--url", srv.URL, "--token", "key_test",
		"--org", "acme", "--project", "main", "--output", "json")
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, out)
	}
	if !srv.sawCredential("key_test") {
		t.Error("the explicit --token must reach the data plane as a bearer credential")
	}
	if !strings.Contains(out, `"hello"`) {
		t.Fatalf("output missing the post: %s", out)
	}
	if !strings.Contains(out, `"data"`) {
		t.Fatalf("list output must use the data envelope: %s", out)
	}
}

func TestBlogList_RendersTable(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK,
		`{"posts":[{"slug":"hello","title":"Hello","status":"published","version":2}]}`)

	out, err := runBlog(t, "blog", "list",
		"--url", srv.URL, "--token", "key_test",
		"--org", "acme", "--project", "main", "--output", "text")
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, out)
	}
	for _, want := range []string{"SLUG", "hello", "published"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q: %s", want, out)
		}
	}
}

// TestBlog_CredentialIsNeverSentToAnotherHost is the host-binding guard. SECURITY: a device credential saved for one instance must not leave for another.
func TestBlog_CredentialIsNeverSentToAnotherHost(t *testing.T) {
	sessPath := setSelfhostedEnv(t)
	issuer := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)
	other := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)

	if err := saveProfile(sessPath, profile{
		URL: issuer.URL, Org: "acme", Project: "main", Device: "key_for_issuer",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := runBlog(t, "blog", "list", "--url", other.URL, "--output", "json")
	if err == nil {
		t.Fatalf("listing against another host with only the issuer's credential must fail: %s", out)
	}
	if other.sawCredential("key_for_issuer") {
		t.Fatal("the issuer's device credential was sent to a different host")
	}
	if other.hits() != 0 {
		t.Fatalf("no request may be made without a credential for that host, got %d", other.hits())
	}
}

// TestBlog_HostMismatchProfileIsRefused covers a stored profile whose recorded URL is not the one being targeted.
func TestBlog_HostMismatchProfileIsRefused(t *testing.T) {
	sessPath := setSelfhostedEnv(t)
	other := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)

	sf := &sessionFile{Profiles: map[string]profile{
		other.URL: {URL: "https://issuer.example", Org: "acme", Project: "main", Device: "key_for_issuer"},
	}}
	if err := saveSessionFile(sessPath, sf); err != nil {
		t.Fatal(err)
	}

	out, err := runBlog(t, "blog", "list", "--url", other.URL, "--output", "json")
	if err == nil {
		t.Fatalf("a profile bound to another host must be refused: %s", out)
	}
	if !IsHostMismatchError(err) {
		t.Fatalf("want a HostMismatchError, got %#v", err)
	}
	if other.sawCredential("key_for_issuer") {
		t.Fatal("the mismatched credential was sent anyway")
	}
}

func TestBlog_UsesSavedProfileForItsOwnHost(t *testing.T) {
	sessPath := setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)

	if err := saveProfile(sessPath, profile{
		URL: srv.URL, Org: "acme", Project: "main", Device: "key_for_srv",
	}); err != nil {
		t.Fatal(err)
	}

	if out, err := runBlog(t, "blog", "list", "--url", srv.URL, "--output", "json"); err != nil {
		t.Fatalf("execute: %v (%s)", err, out)
	}
	if !srv.sawCredential("key_for_srv") {
		t.Error("a profile bound to this host must supply both the credential and the tenant slugs")
	}
}

func TestBlogUpdate_RequiresIfVersion(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK, `{}`)

	if _, err := runBlog(t, "blog", "update", "hello",
		"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main",
		"--title", "New"); err == nil {
		t.Fatal("update without --if-version must be a usage error")
	}
	if srv.hits() != 0 {
		t.Fatal("an unconditional write must never leave the CLI")
	}
}

func TestBlogUpdate_StaleVersionIsItsOwnFailure(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusPreconditionFailed, `{"code":"precondition_failed","message":"precondition_failed"}`)

	_, err := runBlog(t, "blog", "update", "hello",
		"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main",
		"--if-version", "3", "--title", "New", "--output", "json")
	if err == nil {
		t.Fatal("a stale If-Match must fail the command")
	}
	if got := ExitCodeFor(err); got != ExitOnboardingRequired {
		t.Errorf("exit code = %d, want %d (failed precondition)", got, ExitOnboardingRequired)
	}
	if !strings.Contains(err.Error(), "--if-version") {
		t.Errorf("the message must point the operator at the fix, got %q", err.Error())
	}
}

func TestBlogDelete_SendsIfMatch(t *testing.T) {
	setSelfhostedEnv(t)
	var gotMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := runBlog(t, "blog", "delete", "hello",
		"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main",
		"--if-version", "5", "--output", "text")
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, out)
	}
	if gotMatch != `W/"5"` {
		t.Errorf("If-Match = %q, want the weak validator for version 5", gotMatch)
	}
	if !strings.Contains(out, "Deleted post hello") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestBlogCreate_SendsIdempotencyKey(t *testing.T) {
	setSelfhostedEnv(t)
	var gotIdem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdem = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p1","slug":"hello","title":"Hello","status":"draft","version":1}`))
	}))
	defer srv.Close()

	out, err := runBlog(t, "blog", "create",
		"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main",
		"--title", "Hello", "--slug", "hello",
		"--category", "0f1d3a2c-0000-4000-8000-000000000001",
		"--idempotency-key", "idem-1", "--output", "text")
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, out)
	}
	if gotIdem != "idem-1" {
		t.Errorf("Idempotency-Key = %q", gotIdem)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestBlog_NoCredentialIsUnauthenticated(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)

	_, err := runBlog(t, "blog", "list", "--url", srv.URL, "--org", "acme", "--project", "main")
	if err == nil {
		t.Fatal("no credential must fail rather than call anonymously")
	}
	if got := ExitCodeFor(err); got != ExitUnauth {
		t.Errorf("exit code = %d, want %d", got, ExitUnauth)
	}
}

func TestBlog_MissingTenantSlugsFail(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK, `{"posts":[]}`)

	if _, err := runBlog(t, "blog", "list", "--url", srv.URL, "--token", "k"); err == nil {
		t.Fatal("a data plane call needs an org and a project in its path")
	}
	if srv.hits() != 0 {
		t.Fatal("no request may be built from an incomplete tenant path")
	}
}

func TestBlog_SubcommandsTakeNoStrayArgs(t *testing.T) {
	setSelfhostedEnv(t)
	if _, err := runBlog(t, "blog", "list", "stray"); err == nil {
		t.Fatal("blog list takes no positional arguments")
	}
}

func TestBlogPublish_SendsIfMatchToTheSubresource(t *testing.T) {
	for _, tc := range []struct{ command, path, status string }{
		{"publish", "/api/v1/orgs/acme/projects/main/posts/hello/publish", "published"},
		{"unpublish", "/api/v1/orgs/acme/projects/main/posts/hello/unpublish", "draft"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			setSelfhostedEnv(t)
			var gotMethod, gotPath, gotMatch string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotMatch = r.Method, r.URL.Path, r.Header.Get("If-Match")
				_, _ = w.Write([]byte(`{"slug":"hello","status":"` + tc.status + `","version":6}`))
			}))
			defer srv.Close()

			out, err := runBlog(t, "blog", tc.command, "hello",
				"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main",
				"--if-version", "5", "--output", "text")
			if err != nil {
				t.Fatalf("execute: %v (%s)", err, out)
			}
			if gotMethod != http.MethodPost || gotPath != tc.path {
				t.Errorf("%s %s, want POST %s", gotMethod, gotPath, tc.path)
			}
			if gotMatch != `W/"5"` {
				t.Errorf("If-Match = %q, want the weak validator for version 5", gotMatch)
			}
			if !strings.Contains(out, tc.status) {
				t.Errorf("unexpected output: %s", out)
			}
		})
	}
}

func TestBlogPublish_RequiresIfVersion(t *testing.T) {
	setSelfhostedEnv(t)
	srv := newRecordingServer(t, http.StatusOK, `{}`)

	if _, err := runBlog(t, "blog", "publish", "hello",
		"--url", srv.URL, "--token", "k", "--org", "acme", "--project", "main"); err == nil {
		t.Fatal("publish without --if-version must be a usage error")
	}
	if srv.hits() != 0 {
		t.Fatal("an unconditional transition must never leave the CLI")
	}
}

func TestBlogPublish_TakesExactlyOneSlug(t *testing.T) {
	setSelfhostedEnv(t)
	if _, err := runBlog(t, "blog", "publish", "--if-version", "1"); err == nil {
		t.Fatal("blog publish requires a slug")
	}
	if _, err := runBlog(t, "blog", "unpublish", "hello", "stray", "--if-version", "1"); err == nil {
		t.Fatal("blog unpublish takes exactly one positional argument")
	}
}
