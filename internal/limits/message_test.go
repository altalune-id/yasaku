package limits_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
)

func TestMessage_CloudVariant(t *testing.T) {
	e := &limits.ReachedError{Key: limits.OrgProjects, Max: 3, Name: "Acme"}
	assert.Equal(t, "Acme can have 3 projects on the Free plan. Rename a project or wipe its data to reuse it. Need more? Tell us at help@example.com.",
		limits.Message("en-US", config.ModeCloud, e, "help@example.com"))
	assert.Equal(t, "Acme can have 3 projects on the Free plan. Rename a project or wipe its data to reuse it.",
		limits.Message("en-US", config.ModeCloud, e, ""), "no contact drops the Tell us sentence")
}

func TestMessage_SelfHostedVariant(t *testing.T) {
	const suffix = " Ask your administrator."
	tests := []struct {
		key  limits.Key
		want string
	}{
		{limits.OrgMembers, "Acme has 5 seats, all taken (2 pending). Revoke an invite or remove a member to add someone."},
		{limits.OrgProjects, "Acme can have 5 projects. Rename a project or wipe its data to reuse it."},
		{limits.UserOrgsJoined, "You're already in 5 organizations, the most allowed. Leave one to join Acme."},
		{limits.ProjectWallets, "This project has 5 active wallets. Archive one you no longer use to add another."},
		{limits.OrgCustomRoles, "Acme can have 5 custom roles. Delete one to add another."},
	}
	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			e := &limits.ReachedError{Key: tt.key, Max: 5, Pending: 2, Name: "Acme"}
			got := limits.Message("en-US", config.ModeSelfhosted, e, "help@example.com")
			assert.Equal(t, tt.want+suffix, got)
			assert.NotContains(t, got, "Free plan")
		})
	}
}

func TestJoinNotice(t *testing.T) {
	e := &limits.ReachedError{Key: limits.OrgMembers, Max: 5, Pending: 1, Name: "Acme"}
	assert.Equal(t, "Couldn't join Acme: Acme has 5 seats, all taken (1 pending). Revoke an invite or remove a member to add someone.",
		limits.JoinNotice("en-US", config.ModeCloud, e, ""))
}

func TestMessage_PluralizesOnMax(t *testing.T) {
	one := &limits.ReachedError{Key: limits.OrgProjects, Max: 1, Name: "Acme"}
	assert.Equal(t, "Acme can have 1 project on the Free plan. Rename the project or wipe its data to reuse it.",
		limits.Message("en-US", config.ModeCloud, one, ""))
	three := &limits.ReachedError{Key: limits.OrgProjects, Max: 3, Name: "Acme"}
	assert.Equal(t, "Acme can have 3 projects on the Free plan. Rename a project or wipe its data to reuse it.",
		limits.Message("en-US", config.ModeCloud, three, ""))
}

func TestMessage_PlainKeyWithCountOne(t *testing.T) {
	e := &limits.ReachedError{Key: limits.UserOrgsCreated, Max: 1}
	assert.Equal(t, "You can create one organization on the Free plan.", limits.Message("en-US", config.ModeCloud, e, ""))
	joined := &limits.ReachedError{Key: limits.UserOrgsJoined, Max: 1, Name: "Acme"}
	assert.NotContains(t, limits.Message("id-ID", config.ModeCloud, joined, ""), "limits.message", "a locale with a plain string still renders at Max 1")
}
