package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/wallet"
)

type scopeResolver struct {
	orgs     *org.Service
	projects *project.Service
}

type scopeResult struct {
	ctx     context.Context
	org     *org.Org
	project *project.Project
	userID  uuid.UUID
}

// resolve turns a Target into a tenant-scoped context, asking for the missing half through Needs.
// SECURITY: an org the caller does not belong to is reported exactly as an unknown slug is.
func (r scopeResolver) resolve(ctx context.Context, t *yasakuv1.Target) (scopeResult, *yasakuv1.Needs, error) {
	p, err := principal(ctx)
	if err != nil {
		return scopeResult{}, nil, err
	}

	orgs, err := r.orgs.List(ctx, p.UserID)
	if err != nil {
		return scopeResult{}, nil, err
	}
	o, nd, err := pickOrg(orgs, t.GetOrg())
	if err != nil || nd != nil {
		return scopeResult{}, nd, err
	}
	// NOTE: pickOrg already chose from orgs.List(userID), which returns only orgs the caller belongs
	// to, so this check is defence-in-depth and no test at this layer can falsify it; it is kept so a
	// future List that widens does not silently widen the RPC surface with it.
	// NOTE: org.Store.MembershipOf opens a tenanted transaction, so it needs the org scope already
	// on ctx; only org.Store.List is reachable unscoped (its SECURITY DEFINER wrapper lifts RLS).
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: p.UserID})
	if _, mErr := r.orgs.MembershipOf(orgCtx, o.ID, p.UserID); mErr != nil {
		if org.IsMembershipMissingError(mErr) || org.IsNotFoundError(mErr) {
			return scopeResult{}, nil, scopeNotFound("org", o.Slug)
		}
		return scopeResult{}, nil, mErr
	}

	projects, err := r.projects.List(orgCtx, o.ID)
	if err != nil {
		return scopeResult{}, nil, err
	}
	proj, nd, err := pickProject(projects, t.GetProject())
	if err != nil || nd != nil {
		return scopeResult{}, nd, err
	}

	scoped := tenant.Into(ctx, tenant.Context{OrgID: o.ID, ProjectID: proj.ID, UserID: p.UserID})
	return scopeResult{ctx: scoped, org: o, project: proj, userID: p.UserID}, nil, nil
}

func pickOrg(orgs []*org.Org, slug string) (*org.Org, *yasakuv1.Needs, error) {
	slug = strings.TrimSpace(slug)
	if slug != "" {
		for _, o := range orgs {
			if strings.EqualFold(o.Slug, slug) {
				return o, nil, nil
			}
		}
		return nil, nil, scopeNotFound("org", slug)
	}
	switch len(orgs) {
	case 0:
		return nil, needs("org", "you belong to no org"), nil
	case 1:
		return orgs[0], nil, nil
	default:
		return nil, needs("org", "more than one org is available", orgSlugs(orgs)...), nil
	}
}

func pickProject(projects []*project.Project, slug string) (*project.Project, *yasakuv1.Needs, error) {
	slug = strings.TrimSpace(slug)
	if slug != "" {
		for _, p := range projects {
			if strings.EqualFold(p.Slug, slug) {
				return p, nil, nil
			}
		}
		return nil, nil, scopeNotFound("project", slug)
	}
	switch len(projects) {
	case 0:
		return nil, needs("project", "this org has no project"), nil
	case 1:
		return projects[0], nil, nil
	default:
		return nil, needs("project", "more than one project is available", projectSlugs(projects)...), nil
	}
}

func orgSlugs(orgs []*org.Org) []string {
	out := make([]string, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.Slug)
	}
	slices.Sort(out)
	return out
}

func projectSlugs(projects []*project.Project) []string {
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		out = append(out, p.Slug)
	}
	slices.Sort(out)
	return out
}

type nameMatch struct {
	id   uuid.UUID
	name string
}

// NOTE: mirrors the exact-then-substring fold rule wallet.ResolveByName and category.ResolveByName
// both apply, for the row sets neither of them will list.
func matchByName(rows []nameMatch, q string) (matched []nameMatch) {
	needle := strings.ToLower(strings.TrimSpace(q))
	var exact, partial []nameMatch
	for _, r := range rows {
		name := strings.ToLower(r.name)
		switch {
		case name == needle:
			exact = append(exact, r)
		case strings.Contains(name, needle):
			partial = append(partial, r)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

// NOTE: falls back to ids when two rows share a name, or the candidates would be ambiguous again.
func candidateLabels(rows []nameMatch) []string {
	seen := make(map[string]int, len(rows))
	for _, r := range rows {
		seen[strings.ToLower(r.name)]++
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if seen[strings.ToLower(r.name)] > 1 {
			out = append(out, r.id.String())
			continue
		}
		out = append(out, r.name)
	}
	slices.Sort(out)
	return out
}

func walletRows(ctx context.Context, wallets *wallet.Service, opts wallet.ListOpts) ([]nameMatch, error) {
	rows, err := wallets.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make([]nameMatch, 0, len(rows))
	for _, w := range rows {
		out = append(out, nameMatch{id: w.ID, name: w.Name})
	}
	return out, nil
}

func categoryRows(ctx context.Context, cats *category.Service, opts category.ListOpts) ([]nameMatch, error) {
	rows, err := cats.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make([]nameMatch, 0, len(rows))
	for _, c := range rows {
		out = append(out, nameMatch{id: c.ID, name: c.Name})
	}
	return out, nil
}

func resolveWallet(ctx context.Context, wallets *wallet.Service, field, q string) (*wallet.Wallet, *yasakuv1.Needs, error) {
	return resolveWalletIn(ctx, wallets, wallet.ListOpts{}, field, q)
}

// NOTE: wallet.Service.ResolveByName lists active wallets only, so UnarchiveWallet — whose target
// is archived by definition — would be unreachable by name through it.
func resolveArchivedWallet(ctx context.Context, wallets *wallet.Service, field, q string) (*wallet.Wallet, *yasakuv1.Needs, error) {
	return resolveWalletIn(ctx, wallets, wallet.ListOpts{IncludeArchived: true}, field, q)
}

func resolveWalletIn(ctx context.Context, wallets *wallet.Service, opts wallet.ListOpts, field, q string) (*wallet.Wallet, *yasakuv1.Needs, error) {
	q = strings.TrimSpace(q)
	rows, err := walletRows(ctx, wallets, opts)
	if err != nil {
		return nil, nil, err
	}
	if q == "" {
		return nil, needs(field, "name the wallet", candidateLabels(rows)...), nil
	}
	if id, pErr := uuid.Parse(q); pErr == nil {
		w, bErr := wallets.ByID(ctx, id)
		switch {
		case bErr == nil && (opts.IncludeArchived || !w.IsArchived()):
			return w, nil, nil
		case bErr == nil, wallet.IsNotFoundError(bErr):
			return nil, needs(field, "no wallet has that id", candidateLabels(rows)...), nil
		default:
			return nil, nil, bErr
		}
	}

	// The active path delegates to the domain so its rule stays authoritative; only the
	// archived-inclusive path, which the domain does not offer, matches locally.
	if !opts.IncludeArchived {
		w, rErr := wallets.ResolveByName(ctx, q)
		switch {
		case rErr == nil:
			return w, nil, nil
		case wallet.IsNotFoundError(rErr):
			return nil, needs(field, "no wallet named "+q, candidateLabels(rows)...), nil
		default:
			if amb, ok := errors.AsType[*wallet.AmbiguousNameError](rErr); ok {
				return nil, needs(field, q+" matches more than one wallet", amb.Candidates...), nil
			}
			return nil, nil, rErr
		}
	}

	matched := matchByName(rows, q)
	switch len(matched) {
	case 0:
		return nil, needs(field, "no wallet named "+q, candidateLabels(rows)...), nil
	case 1:
		w, bErr := wallets.ByID(ctx, matched[0].id)
		if bErr != nil {
			return nil, nil, bErr
		}
		return w, nil, nil
	default:
		return nil, needs(field, q+" matches more than one wallet", candidateLabels(matched)...), nil
	}
}

func resolveCategory(ctx context.Context, cats *category.Service, kind category.Kind, field, q string) (*category.Category, *yasakuv1.Needs, error) {
	return resolveCategoryIn(ctx, cats, category.ListOpts{Kind: kind}, field, q)
}

// SECURITY: it must never pick a kind for the caller — a name held by both an expense and an
// income category is ambiguous, and answering with one of them would act on the wrong row.
func resolveCategoryAnyKind(ctx context.Context, cats *category.Service, field, q string) (*category.Category, *yasakuv1.Needs, error) {
	return resolveCategoryIn(ctx, cats, category.ListOpts{}, field, q)
}

// NOTE: category.Service.ResolveByName lists active categories only, so UnarchiveCategory's
// target would be unreachable by name through it.
func resolveArchivedCategoryAnyKind(ctx context.Context, cats *category.Service, field, q string) (*category.Category, *yasakuv1.Needs, error) {
	return resolveCategoryIn(ctx, cats, category.ListOpts{IncludeArchived: true}, field, q)
}

func resolveCategoryIn(ctx context.Context, cats *category.Service, opts category.ListOpts, field, q string) (*category.Category, *yasakuv1.Needs, error) {
	q = strings.TrimSpace(q)
	rows, err := categoryRows(ctx, cats, opts)
	if err != nil {
		return nil, nil, err
	}
	if q == "" {
		return nil, needs(field, "name the category", candidateLabels(rows)...), nil
	}
	if id, pErr := uuid.Parse(q); pErr == nil {
		c, bErr := cats.ByID(ctx, id)
		switch {
		case bErr == nil && categoryInScope(c, opts):
			return c, nil, nil
		case bErr == nil, category.IsNotFoundError(bErr):
			return nil, needs(field, "no category has that id", candidateLabels(rows)...), nil
		default:
			return nil, nil, bErr
		}
	}
	matched := matchByName(rows, q)
	switch len(matched) {
	case 0:
		return nil, needs(field, "no category named "+q, candidateLabels(rows)...), nil
	case 1:
		c, bErr := cats.ByID(ctx, matched[0].id)
		if bErr != nil {
			return nil, nil, bErr
		}
		return c, nil, nil
	default:
		return nil, needs(field, q+" matches more than one category", candidateLabels(matched)...), nil
	}
}

func categoryInScope(c *category.Category, opts category.ListOpts) bool {
	if opts.Kind != "" && c.Kind != opts.Kind {
		return false
	}
	return opts.IncludeArchived || !c.IsArchived()
}

// NOTE: the contract documents `period` as an id everywhere, so a name is refused, never guessed.
func resolvePeriod(ctx context.Context, periods *period.Service, q string) (*period.Period, *yasakuv1.Needs, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		p, err := periods.Current(ctx)
		if err != nil {
			return nil, nil, err
		}
		return p, nil, nil
	}
	id, err := uuid.Parse(q)
	if err != nil {
		return periodNeeds(ctx, periods, "period must be a period id from list_periods or current_period, not a name")
	}
	p, err := periods.ByID(ctx, id)
	if err != nil {
		if period.IsNotFoundError(err) {
			return periodNeeds(ctx, periods, "no period has that id")
		}
		return nil, nil, err
	}
	return p, nil, nil
}

func periodNeeds(ctx context.Context, periods *period.Service, reason string) (*period.Period, *yasakuv1.Needs, error) {
	rows, err := periods.List(ctx, period.ListOpts{})
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.ID.String())
	}
	return nil, needs("period", reason, ids...), nil
}

// NOTE: period.Service.Containing creates the first period; a read path must never do that.
func periodContaining(ctx context.Context, periods *period.Service, at time.Time, loc *time.Location) (*period.Period, error) {
	rows, err := periods.List(ctx, period.ListOpts{})
	if err != nil {
		return nil, err
	}
	d := civil.DateOf(at, loc)
	for _, p := range rows {
		if p.Contains(d) {
			return p, nil
		}
	}
	return nil, nil
}
