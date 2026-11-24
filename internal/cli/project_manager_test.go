package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
)

// SECURITY: the local CLI calls the project service directly, so the service gate is what stops a plain member creating a project.
func TestProjectCreate_LocalMemberIsRefused(t *testing.T) {
	sessPath := setSelfhostedEnv(t)
	clearCLIEnv(t)
	ctx := context.Background()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := boot.BootServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ownerUser, err := srv.Users.Create(ctx, user.CreateRequest{Email: "owner@example.com", Name: "Owner", Source: user.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerUser.ID
	o, err := srv.Orgs.BootstrapSingleton(ctx, "acme", "Acme", owner)
	if err != nil {
		t.Fatal(err)
	}
	member, err := srv.Users.Create(ctx, user.CreateRequest{Email: "member@example.com", Name: "Member", Source: user.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Orgs.AddMember(tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner}), o.ID, member.ID, org.RoleMember); err != nil {
		t.Fatal(err)
	}
	_ = srv.Close()

	bootFn := func(ctx context.Context, cfg *config.Config, _ ...boot.Option) (*boot.Server, error) {
		return boot.BootServer(ctx, cfg)
	}
	create := func(userID, slug string) (string, error) {
		writeRemoteProfile(t, sessPath, defaultProfile, profile{Principal: principalView{UserID: userID, ActiveOrgID: o.ID.String(), Source: "local"}})
		root := NewRootCmd(bootFn, stubClientBoot)
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs([]string{"project", "create", "--slug", slug, "--name", "Side"})
		err := root.ExecuteContext(ctx)
		return buf.String(), err
	}

	_, err = create(member.ID.String(), "side")
	if !authz.IsDeniedError(err) || !strings.HasPrefix(ErrorLine(err), "ROL001: ") {
		t.Fatalf("member project create err = %v (%q), want a ROL001 refusal", err, ErrorLine(err))
	}
	out, err := create(owner.String(), "side")
	if err != nil || !strings.Contains(out, "Created project side") {
		t.Fatalf("owner project create: %v (%s)", err, out)
	}
}

// SECURITY: the CLI binds through the resolver like every surface, so a member lists invites but cannot send one.
func TestInviteCLI_MemberListsButCannotSend(t *testing.T) {
	sessPath := setSelfhostedEnv(t)
	clearCLIEnv(t)
	ctx := context.Background()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.OIDC.Issuer = "https://idp.example.com"
	srv, err := boot.BootServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ownerUser, err := srv.Users.Create(ctx, user.CreateRequest{Email: "owner@example.com", Name: "Owner", Source: user.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	o, err := srv.Orgs.BootstrapSingleton(ctx, "acme", "Acme", ownerUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := srv.Users.Create(ctx, user.CreateRequest{Email: "member@example.com", Name: "Member", Source: user.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Orgs.AddMember(tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: ownerUser.ID}), o.ID, member.ID, org.RoleMember); err != nil {
		t.Fatal(err)
	}
	_ = srv.Close()

	bootFn := func(ctx context.Context, c *config.Config, _ ...boot.Option) (*boot.Server, error) {
		c.OIDC.Issuer = cfg.OIDC.Issuer
		return boot.BootServer(ctx, c)
	}
	run := func(args ...string) error {
		writeRemoteProfile(t, sessPath, defaultProfile, profile{Principal: principalView{UserID: member.ID.String(), ActiveOrgID: o.ID.String(), Source: "local"}})
		root := NewRootCmd(bootFn, stubClientBoot)
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(args)
		return root.ExecuteContext(ctx)
	}
	if err := run("invite", "list"); err != nil {
		t.Fatalf("a member lists invites: %v", err)
	}
	if err := run("invite", "send", "--email", "new@example.com", "--role", "member"); !authz.IsDeniedError(err) || !strings.HasPrefix(ErrorLine(err), "ROL001: ") {
		t.Fatalf("member invite send err = %v (%q), want a ROL001 refusal", err, ErrorLine(err))
	}
}
