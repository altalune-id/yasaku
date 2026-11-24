package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "admin",
		Short:   "Operator tasks on this deployment's database",
		GroupID: "runtime",
	}
	cmd.AddCommand(newAdminLimitsCmd())
	return cmd
}

func newAdminLimitsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "limits",
		Short: "List and change plan limits and per-subject overrides",
	}
	cmd.AddCommand(newAdminLimitsListCmd(), newAdminLimitsSetCmd(), newAdminLimitsUnsetCmd())
	return cmd
}

type limitTarget struct {
	plan, org, user, project string
}

func (f *limitTarget) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.plan, "plan", limits.DefaultPlan, "plan whose limit to change, when no subject is named")
	cmd.Flags().StringVar(&f.org, "org", "", "org slug to override")
	cmd.Flags().StringVar(&f.user, "user", "", "user email to override")
	cmd.Flags().StringVar(&f.project, "project", "", "project id to override")
	cmd.MarkFlagsMutuallyExclusive("org", "user", "project")
}

type adminDB struct {
	conn  *sql.DB
	cfg   db.DBConfig
	store limits.Store
}

func openAdminDB(cmd *cobra.Command) (*adminDB, error) {
	cfg, err := withCfg(cmd)
	if err != nil {
		return nil, err
	}
	conn, migCfg, err := openMigratorDB(cmd.Context(), cfg)
	if err != nil {
		return nil, err
	}
	return &adminDB{conn: conn, cfg: migCfg.DB, store: limits.NewStore(migCfg.DB, db.Pool{W: conn, R: conn})}, nil
}

func (a *adminDB) table(name string) string {
	if a.cfg.Driver == db.DriverPostgres {
		return a.cfg.Schema + "." + a.cfg.TablePrefix + name
	}
	return a.cfg.TablePrefix + name
}

func (a *adminDB) lookup(ctx context.Context, table, nameColumn, column, value string) (uuid.UUID, string, error) {
	q := "SELECT id, " + nameColumn + " FROM " + a.table(table) + " WHERE " + column + " = ?"
	if a.cfg.Driver == db.DriverPostgres {
		q = strings.Replace(q, "?", "$1", 1)
	}
	var raw, name string
	if err := a.conn.QueryRowContext(ctx, q, value).Scan(&raw, &name); err != nil {
		return uuid.Nil, "", fmt.Errorf("admin limits: %s %q: %w", table, value, err)
	}
	id, err := uuid.Parse(raw)
	return id, name, err
}

func (a *adminDB) audited(ctx context.Context, e audit.Event, write func(ctx context.Context) error) error {
	var raw string
	q := "SELECT id FROM " + a.table("orgs") + " WHERE system ORDER BY created_at, id LIMIT 1"
	if a.cfg.Driver != db.DriverPostgres {
		q = "SELECT id FROM " + a.table("orgs") + " WHERE system = 1 ORDER BY created_at, id LIMIT 1"
	}
	if err := a.conn.QueryRowContext(ctx, q).Scan(&raw); err != nil {
		return &NoSystemOrgError{Cause: err}
	}
	sysID, err := uuid.Parse(raw)
	if err != nil {
		return err
	}
	pool := db.Pool{W: a.conn, R: a.conn}
	pc := tenant.NewPgConn(a.conn)
	rec := audit.NewRecorder(a.cfg, pool, pc)
	uow := tenant.NewUnitOfWork(a.cfg, pool, pc)
	return uow(tenant.Into(ctx, tenant.Context{OrgID: sysID}), func(ctx context.Context) error {
		if err := write(ctx); err != nil {
			return err
		}
		return rec.Record(ctx, e)
	})
}

// NOTE: no subject flag means the plan row; a subject flag means an override for that subject, returned with the name its audit row shows.
func (a *adminDB) subject(ctx context.Context, t limitTarget) (*limits.Subject, string, error) {
	switch {
	case t.org != "":
		id, name, err := a.lookup(ctx, "orgs", "name", "slug", t.org)
		return &limits.Subject{Kind: limits.SubjectOrg, ID: id}, name, err
	case t.user != "":
		id, email, err := a.lookup(ctx, "users", "email", "email", strings.ToLower(strings.TrimSpace(t.user)))
		return &limits.Subject{Kind: limits.SubjectUser, ID: id}, email, err
	case t.project != "":
		pid, err := uuid.Parse(t.project)
		if err != nil {
			return nil, "", fmt.Errorf("admin limits: project id: %w", err)
		}
		id, name, err := a.lookup(ctx, "projects", "name", "id", pid.String())
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", &UnknownProjectError{ID: t.project}
		}
		return &limits.Subject{Kind: limits.SubjectProject, ID: id}, name, err
	default:
		return nil, "", nil
	}
}

func checkTarget(key limits.Key, kind limits.SubjectKind) error {
	def, ok := limits.Lookup(key)
	if !ok {
		return &limits.UnknownKeyError{Key: key}
	}
	if kind != "" && kind != def.Subject {
		return &limits.SubjectMismatchError{Key: key, Want: def.Subject, Got: kind}
	}
	return nil
}

func newAdminLimitsSetCmd() *cobra.Command {
	var target limitTarget
	cmd := &cobra.Command{
		Use:   "set <key> <value|unlimited>",
		Short: "Set a plan limit, or override it for one org, user or project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := limits.Key(args[0])
			value, err := parseLimitValue(args[1])
			if err != nil {
				return err
			}
			kind := targetKind(target)
			if err := checkTarget(key, kind); err != nil {
				return err
			}
			a, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = a.conn.Close() }()
			sub, name, err := a.subject(cmd.Context(), target)
			if err != nil {
				return err
			}
			if sub == nil {
				e := audit.Event{Action: "limit.plan_set", TargetKind: "plan", TargetID: target.plan,
					Detail: map[string]string{"plan": target.plan, "key": string(key), "value": formatLimit(value)}}
				return a.audited(cmd.Context(), e, func(ctx context.Context) error {
					return a.store.SetPlan(ctx, target.plan, key, value, uuid.Nil)
				})
			}
			e := audit.Event{Action: "limit.override_set", TargetKind: string(sub.Kind), TargetID: sub.ID.String(),
				Detail: map[string]string{"name": name, "subject_kind": string(sub.Kind), "subject_id": sub.ID.String(), "key": string(key), "value": formatLimit(value)}}
			return a.audited(cmd.Context(), e, func(ctx context.Context) error {
				return a.store.SetOverride(ctx, *sub, key, value, uuid.Nil)
			})
		},
	}
	target.bind(cmd)
	return cmd
}

func newAdminLimitsUnsetCmd() *cobra.Command {
	var target limitTarget
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear a plan limit or delete an override, reverting to the default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := limits.Key(args[0])
			if err := checkTarget(key, targetKind(target)); err != nil {
				return err
			}
			a, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = a.conn.Close() }()
			sub, name, err := a.subject(cmd.Context(), target)
			if err != nil {
				return err
			}
			if sub == nil {
				e := audit.Event{Action: "limit.plan_set", TargetKind: "plan", TargetID: target.plan,
					Detail: map[string]string{"plan": target.plan, "key": string(key), "value": "default"}}
				return a.audited(cmd.Context(), e, func(ctx context.Context) error {
					return a.store.ClearPlan(ctx, target.plan, key)
				})
			}
			e := audit.Event{Action: "limit.override_deleted", TargetKind: string(sub.Kind), TargetID: sub.ID.String(),
				Detail: map[string]string{"name": name, "subject_kind": string(sub.Kind), "subject_id": sub.ID.String(), "key": string(key)}}
			return a.audited(cmd.Context(), e, func(ctx context.Context) error {
				return a.store.DeleteOverride(ctx, *sub, key)
			})
		},
	}
	target.bind(cmd)
	return cmd
}

func newAdminLimitsListCmd() *cobra.Command {
	var plan string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Print the catalogue defaults, the plan's rows and every override",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := withCfg(cmd)
			if err != nil {
				return err
			}
			a, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = a.conn.Close() }()
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "# defaults ("+string(cfg.Mode)+")")
			for _, d := range limits.Catalogue() {
				_, _ = fmt.Fprintf(out, "%s\t%s\t%s\n", d.Key, d.Subject, formatLimit(modeDefault(d, cfg.Mode)))
			}
			rows, err := a.store.ListPlan(cmd.Context(), plan)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out, "# plan "+plan)
			for _, r := range rows {
				_, _ = fmt.Fprintf(out, "%s\t%s\t%s\n", r.Plan, r.Key, formatLimit(r.Value))
			}
			overrides, err := a.store.ListOverrides(cmd.Context())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out, "# overrides")
			for _, o := range overrides {
				_, _ = fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", o.Subject.Kind, o.Subject.ID, o.Key, formatLimit(o.Value))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&plan, "plan", limits.DefaultPlan, "plan whose rows to print")
	return cmd
}

func targetKind(t limitTarget) limits.SubjectKind {
	switch {
	case t.org != "":
		return limits.SubjectOrg
	case t.user != "":
		return limits.SubjectUser
	case t.project != "":
		return limits.SubjectProject
	default:
		return ""
	}
}

func modeDefault(d limits.Def, mode config.Mode) int64 {
	if mode == config.ModeCloud {
		return d.Cloud
	}
	return d.SelfHosted
}

func parseLimitValue(raw string) (int64, error) {
	if raw == "unlimited" {
		return limits.Unlimited, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("admin limits: value %q must be a non-negative integer or \"unlimited\"", raw)
	}
	return v, nil
}

func formatLimit(v int64) string {
	if v == limits.Unlimited {
		return "unlimited"
	}
	return strconv.FormatInt(v, 10)
}
