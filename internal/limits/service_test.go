package limits_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func TestService_CheckResolution(t *testing.T) {
	org := limits.Subject{Kind: limits.SubjectOrg, ID: uuid.New()}
	project := limits.Subject{Kind: limits.SubjectProject, ID: uuid.New()}
	ctx := context.Background()

	tests := []struct {
		name    string
		mode    config.Mode
		setup   func(*fakes.Limits)
		key     limits.Key
		sub     limits.Subject
		used    int64
		reached bool
		max     int64
		hidden  bool
	}{
		{name: "cloud default", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 3, reached: true, max: 3},
		{name: "under the default", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 2},
		{name: "plan row wins over default", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 3,
			setup: func(f *fakes.Limits) {
				require.NoError(t, f.SetPlan(ctx, limits.DefaultPlan, limits.OrgProjects, 5, uuid.Nil))
			}},
		{name: "plan of the org", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 1, reached: true, max: 1,
			setup: func(f *fakes.Limits) {
				f.SetOrgPlan(org.ID, "pro")
				require.NoError(t, f.SetPlan(ctx, "pro", limits.OrgProjects, 1, uuid.Nil))
			}},
		{name: "override wins over plan", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 4, reached: true, max: 4,
			setup: func(f *fakes.Limits) {
				require.NoError(t, f.SetPlan(ctx, limits.DefaultPlan, limits.OrgProjects, 10, uuid.Nil))
				require.NoError(t, f.SetOverride(ctx, org, limits.OrgProjects, 4, uuid.Nil))
			}},
		{name: "null override is unlimited", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 1000,
			setup: func(f *fakes.Limits) {
				require.NoError(t, f.SetOverride(ctx, org, limits.OrgProjects, limits.Unlimited, uuid.Nil))
			}},
		{name: "deleted override reverts", mode: config.ModeCloud, key: limits.OrgProjects, sub: org, used: 3, reached: true, max: 3,
			setup: func(f *fakes.Limits) {
				require.NoError(t, f.SetOverride(ctx, org, limits.OrgProjects, 50, uuid.Nil))
				require.NoError(t, f.DeleteOverride(ctx, org, limits.OrgProjects))
			}},
		{name: "self-hosted unlimited", mode: config.ModeSelfhosted, key: limits.OrgProjects, sub: org, used: 1000},
		{name: "hidden key", mode: config.ModeCloud, key: limits.ProjectWallets, sub: project, used: 20, reached: true, max: 20, hidden: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := fakes.NewLimits()
			if tt.setup != nil {
				tt.setup(store)
			}
			svc := limits.NewService(store, tt.mode)
			err := svc.Check(ctx, tt.key, tt.sub, tt.used)
			if !tt.reached {
				require.NoError(t, err)
				return
			}
			re, ok := errors.AsType[*limits.ReachedError](err)
			require.True(t, ok, "want *ReachedError, got %T: %v", err, err)
			assert.Equal(t, tt.key, re.Key)
			assert.Equal(t, tt.max, re.Max)
			assert.Equal(t, limits.ReasonLimit, re.Reason)
			assert.Equal(t, tt.hidden, re.Hidden)
		})
	}
}

func TestService_CheckUnknownKey(t *testing.T) {
	svc := limits.NewService(fakes.NewLimits(), config.ModeCloud)
	err := svc.Check(context.Background(), "nope.never", limits.Subject{Kind: limits.SubjectOrg, ID: uuid.New()}, 0)
	assert.True(t, limits.IsUnknownKeyError(err), "got %T: %v", err, err)
}

func TestLock_RefusesOutsideUnitOfWork(t *testing.T) {
	store := fakes.NewLimits()
	svc := limits.NewService(store, config.ModeCloud)
	err := svc.Lock(context.Background(), limits.Subject{Kind: limits.SubjectOrg, ID: uuid.New()})
	assert.True(t, limits.IsNoUnitOfWorkError(err), "got %T: %v", err, err)
	assert.Empty(t, store.Locked, "no row may be locked outside a transaction")
}

func TestLock_SortsSubjects(t *testing.T) {
	store := fakes.NewLimits()
	svc := limits.NewService(store, config.ModeCloud)
	user := limits.Subject{Kind: limits.SubjectUser, ID: uuid.New()}
	org := limits.Subject{Kind: limits.SubjectOrg, ID: uuid.New()}
	project := limits.Subject{Kind: limits.SubjectProject, ID: uuid.New()}

	require.NoError(t, fakes.UnitOfWork(context.Background(), func(ctx context.Context) error {
		return svc.Lock(ctx, project, org, user)
	}))
	require.Len(t, store.Locked, 1)
	assert.Equal(t, []limits.Subject{user, org, project}, store.Locked[0])
}

func TestCheck_NamesTheOrg(t *testing.T) {
	store := fakes.NewLimits()
	org := limits.Subject{Kind: limits.SubjectOrg, ID: uuid.New()}
	store.SetOrgName(org.ID, "Acme")
	err := limits.NewService(store, config.ModeCloud).Check(context.Background(), limits.OrgProjects, org, 3)
	re, ok := errors.AsType[*limits.ReachedError](err)
	require.True(t, ok, "got %T: %v", err, err)
	assert.Equal(t, "Acme", re.Name)
}
