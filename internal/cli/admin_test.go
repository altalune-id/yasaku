package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/limits"
)

func runAdmin(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runRoot(t, append([]string{"admin", "limits"}, args...)...)
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd(stubServerBoot, stubClientBoot)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func envDSN(t *testing.T) string {
	t.Helper()
	return os.Getenv("YASAKU_DB_DSN")
}

func TestAdminLimitsSet_PlanAndOverride(t *testing.T) {
	setSelfhostedEnv(t)
	dsn := envDSN(t)
	if _, err := runRoot(t, "migrate", "up"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	orgID := seedOrg(t, conn, "acme", false)
	seedOrg(t, conn, "system", true)

	if _, err := runAdmin(t, "set", "org.projects", "7"); err != nil {
		t.Fatalf("set plan row: %v", err)
	}
	if _, err := runAdmin(t, "set", "org.projects", "unlimited", "--org", "acme"); err != nil {
		t.Fatalf("set override: %v", err)
	}
	out, err := runAdmin(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first\torg.projects\t7", "org\t" + orgID.String() + "\torg.projects\tunlimited"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output misses %q:\n%s", want, out)
		}
	}

	var value sql.NullInt64
	if err := conn.QueryRow("SELECT value FROM yasaku_limit_overrides WHERE subject_id = ?", orgID.String()).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value.Valid {
		t.Fatalf("unlimited must store NULL, got %d", value.Int64)
	}
	if _, err := runAdmin(t, "unset", "org.projects", "--org", "acme"); err != nil {
		t.Fatalf("unset override: %v", err)
	}
	if err := conn.QueryRow("SELECT value FROM yasaku_limit_overrides WHERE subject_id = ?", orgID.String()).Scan(&value); err != sql.ErrNoRows {
		t.Fatalf("unset must delete the override, got %v", err)
	}
}

func seedOrg(t *testing.T, conn *sql.DB, slug string, system bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sys := 0
	if system {
		sys = 1
	}
	if _, err := conn.Exec("INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at, system) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id.String(), slug, slug, uuid.NewString(), now, now, sys); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAdminLimitsCLI_AuditsUnderSystemOrg(t *testing.T) {
	setSelfhostedEnv(t)
	if _, err := runRoot(t, "migrate", "up"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	conn, err := sql.Open("sqlite", envDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := runAdmin(t, "set", "org.projects", "4"); !IsNoSystemOrgError(err) {
		t.Fatalf("no system org yet must refuse, got %T: %v", err, err)
	}
	sysID := seedOrg(t, conn, "system", true)
	acme := seedOrg(t, conn, "acme", false)
	for _, args := range [][]string{
		{"set", "org.projects", "4"},
		{"set", "org.projects", "9", "--org", "acme"},
		{"unset", "org.projects", "--org", "acme"},
	} {
		if _, err := runAdmin(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	rows, err := conn.Query("SELECT org_id, action, actor_kind, target_id, detail FROM yasaku_audit_events ORDER BY occurred_at")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var org, action, actor, target, detail string
		if err := rows.Scan(&org, &action, &actor, &target, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(action, "limit.override") && !strings.Contains(detail, `"name":"acme"`) {
			t.Errorf("%s detail %s must name the org by slug", action, detail)
		}
		if org != sysID.String() || actor != "system" {
			t.Errorf("%s recorded under org %s by %s, want the system org by system", action, org, actor)
		}
		got = append(got, action+":"+target)
	}
	want := []string{"limit.plan_set:first", "limit.override_set:" + acme.String(), "limit.override_deleted:" + acme.String()}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("audit rows = %v, want %v", got, want)
	}
}

func TestAdminLimitsSet_UnknownKeyRefused(t *testing.T) {
	setSelfhostedEnv(t)
	if _, err := runRoot(t, "migrate", "up"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	_, err := runAdmin(t, "set", "org.nope", "3")
	if !limits.IsUnknownKeyError(err) {
		t.Fatalf("want *limits.UnknownKeyError, got %T: %v", err, err)
	}
	_, err = runAdmin(t, "set", "org.projects", "3", "--user", "someone@example.com")
	if !limits.IsSubjectMismatchError(err) {
		t.Fatalf("an org key on a user subject must be refused, got %T: %v", err, err)
	}
}

func TestAdminLimitsSet_ProjectNamedOrRefused(t *testing.T) {
	setSelfhostedEnv(t)
	if _, err := runRoot(t, "migrate", "up"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	conn, err := sql.Open("sqlite", envDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	seedOrg(t, conn, "system", true)
	acme := seedOrg(t, conn, "acme", false)
	projectID := uuid.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := conn.Exec("INSERT INTO yasaku_projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'books', 'Home Books', ?, ?, ?)",
		projectID.String(), acme.String(), uuid.NewString(), now, now); err != nil {
		t.Fatal(err)
	}

	if _, err := runAdmin(t, "set", "project.wallets", "30", "--project", uuid.NewString()); !IsUnknownProjectError(err) {
		t.Fatalf("an unknown project id must be refused, got %T: %v", err, err)
	}
	if _, err := runAdmin(t, "set", "project.wallets", "30", "--project", projectID.String()); err != nil {
		t.Fatal(err)
	}
	var detail string
	if err := conn.QueryRow("SELECT detail FROM yasaku_audit_events WHERE action = 'limit.override_set'").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, `"name":"Home Books"`) {
		t.Fatalf("the audit row must name the project, got %s", detail)
	}
}
