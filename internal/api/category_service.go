package api

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"connectrpc.com/connect"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/project"
)

//nolint:gochecknoglobals // a compiled pattern, not runtime state.
var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// CategoryService implements yasaku.v1.CategoryService.
type CategoryService struct {
	scope scopeResolver
	cats  *category.Service
}

// NewCategoryService binds the handler to its collaborators.
func NewCategoryService(orgs *org.Service, projects *project.Service, cats *category.Service) *CategoryService {
	return &CategoryService{scope: scopeResolver{orgs: orgs, projects: projects}, cats: cats}
}

// ListCategories returns the project's categories, optionally filtered by kind.
func (s *CategoryService) ListCategories(ctx context.Context, req *connect.Request[yasakuv1.ListCategoriesRequest]) (*connect.Response[yasakuv1.ListCategoriesResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	opts := category.ListOpts{IncludeArchived: req.Msg.GetIncludeArchived()}
	if raw := strings.TrimSpace(req.Msg.GetKind()); raw != "" {
		kind, pErr := category.ParseKind(raw)
		if pErr != nil {
			return nil, pErr
		}
		opts.Kind = kind
	}
	rows, err := s.cats.List(sc.ctx, opts)
	if err != nil {
		return nil, err
	}
	out := &yasakuv1.ListCategoriesResponse{Categories: make([]*yasakuv1.Category, 0, len(rows))}
	for _, c := range rows {
		out.Categories = append(out.Categories, toProtoCategory(c))
	}
	return connect.NewResponse(out), nil
}

// CreateCategory previews, then on confirm persists, one category.
func (s *CategoryService) CreateCategory(ctx context.Context, req *connect.Request[yasakuv1.CreateCategoryRequest]) (*connect.Response[yasakuv1.CreateCategoryResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.CreateCategoryResponse{Needs: nd}), nil
	}
	kind, err := category.ParseKind(strings.TrimSpace(req.Msg.GetKind()))
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return connect.NewResponse(&yasakuv1.CreateCategoryResponse{
			Needs: needs("name", "name the category"),
		}), nil
	}
	if nd := checkIconAndColor(req.Msg.GetIcon(), req.Msg.GetColor()); nd != nil {
		return connect.NewResponse(&yasakuv1.CreateCategoryResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.CreateCategoryResponse{
			Preview: &yasakuv1.Category{
				Name:  name,
				Kind:  string(kind),
				Icon:  req.Msg.GetIcon(),
				Color: req.Msg.GetColor(),
			},
		}), nil
	}
	c, err := s.cats.Create(sc.ctx, name, kind, req.Msg.GetIcon(), req.Msg.GetColor())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.CreateCategoryResponse{Result: toProtoCategory(c)}), nil
}

// RenameCategory replaces the identified category's display name.
func (s *CategoryService) RenameCategory(ctx context.Context, req *connect.Request[yasakuv1.RenameCategoryRequest]) (*connect.Response[yasakuv1.RenameCategoryResponse], error) {
	sc, c, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetCategory())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	out, err := s.cats.Rename(sc.ctx, c.ID, strings.TrimSpace(req.Msg.GetName()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.RenameCategoryResponse{Category: toProtoCategory(out)}), nil
}

// UpdateCategory replaces the icon and colour together; an empty field clears it.
func (s *CategoryService) UpdateCategory(ctx context.Context, req *connect.Request[yasakuv1.UpdateCategoryRequest]) (*connect.Response[yasakuv1.UpdateCategoryResponse], error) {
	sc, c, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetCategory())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	if nd := checkIconAndColor(req.Msg.GetIcon(), req.Msg.GetColor()); nd != nil {
		return nil, needsErr(nd)
	}
	out, err := s.cats.Update(sc.ctx, c.ID, req.Msg.GetIcon(), req.Msg.GetColor())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.UpdateCategoryResponse{Category: toProtoCategory(out)}), nil
}

// ArchiveCategory hides the identified category from new transactions.
func (s *CategoryService) ArchiveCategory(ctx context.Context, req *connect.Request[yasakuv1.ArchiveCategoryRequest]) (*connect.Response[yasakuv1.ArchiveCategoryResponse], error) {
	sc, c, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetCategory())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	out, err := s.cats.Archive(sc.ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.ArchiveCategoryResponse{Category: toProtoCategory(out)}), nil
}

// UnarchiveCategory restores an archived category.
func (s *CategoryService) UnarchiveCategory(ctx context.Context, req *connect.Request[yasakuv1.UnarchiveCategoryRequest]) (*connect.Response[yasakuv1.UnarchiveCategoryResponse], error) {
	sc, c, nd, err := s.lookupArchived(ctx, req.Msg.GetTarget(), req.Msg.GetCategory())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	out, err := s.cats.Unarchive(sc.ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.UnarchiveCategoryResponse{Category: toProtoCategory(out)}), nil
}

// DeleteCategory removes a category that no transaction references.
func (s *CategoryService) DeleteCategory(ctx context.Context, req *connect.Request[yasakuv1.DeleteCategoryRequest]) (*connect.Response[yasakuv1.DeleteCategoryResponse], error) {
	sc, c, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetCategory())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	if err := s.cats.Delete(sc.ctx, c.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.DeleteCategoryResponse{}), nil
}

// SeedDefaultCategories previews, then on confirm inserts, the default category set.
func (s *CategoryService) SeedDefaultCategories(ctx context.Context, req *connect.Request[yasakuv1.SeedDefaultCategoriesRequest]) (*connect.Response[yasakuv1.SeedDefaultCategoriesResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.SeedDefaultCategoriesResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		count, cErr := s.pendingDefaults(sc.ctx)
		if cErr != nil {
			return nil, cErr
		}
		return connect.NewResponse(&yasakuv1.SeedDefaultCategoriesResponse{PreviewCount: count}), nil
	}
	inserted, err := s.cats.SeedDefaults(sc.ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.SeedDefaultCategoriesResponse{
		Inserted: int32(inserted), //nolint:gosec // bounded by len(category.Defaults).
	}), nil
}

func (s *CategoryService) lookup(ctx context.Context, t *yasakuv1.Target, q string) (scopeResult, *category.Category, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, nd, err
	}
	c, nd, err := resolveCategoryAnyKind(sc.ctx, s.cats, "category", q)
	return sc, c, nd, err
}

func (s *CategoryService) lookupArchived(ctx context.Context, t *yasakuv1.Target, q string) (scopeResult, *category.Category, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, nd, err
	}
	c, nd, err := resolveArchivedCategoryAnyKind(sc.ctx, s.cats, "category", q)
	return sc, c, nd, err
}

// NOTE: mirrors category.Service.SeedDefaults' fold-by-name rule because the domain has no dry run.
func (s *CategoryService) pendingDefaults(ctx context.Context) (int32, error) {
	rows, err := s.cats.List(ctx, category.ListOpts{IncludeArchived: true})
	if err != nil {
		return 0, err
	}
	taken := make(map[category.Kind]map[string]bool, 2)
	for _, c := range rows {
		if taken[c.Kind] == nil {
			taken[c.Kind] = map[string]bool{}
		}
		taken[c.Kind][category.FoldName(c.Name)] = true
	}
	var count int32
	for _, d := range category.Defaults {
		name := category.FoldName(defaultCategoryName(ctx, d))
		if taken[d.Kind] != nil && taken[d.Kind][name] {
			continue
		}
		if taken[d.Kind] == nil {
			taken[d.Kind] = map[string]bool{}
		}
		taken[d.Kind][name] = true
		count++
	}
	return count, nil
}

func defaultCategoryName(ctx context.Context, d category.Default) string {
	if t := i18n.TranslatorFrom(ctx); t != nil {
		if name := strings.TrimSpace(t.T(category.DefaultNameKey(d.Key))); name != "" {
			return name
		}
	}
	words := strings.FieldsFunc(d.Key, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, w := range words {
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

//nolint:gochecknoglobals // a fixed table, not runtime state.
var chartColors = []string{"chart-1", "chart-2", "chart-3", "chart-4", "chart-5"}

// NOTE: mirrors category.normalizeColor, which is unexported; see docs/BACKLOG.md.
func checkIconAndColor(icon, color string) *yasakuv1.Needs {
	if icon = strings.TrimSpace(icon); icon != "" && !slices.Contains(category.AllowedIcons, icon) {
		return needs("icon", icon+" is not an accepted icon", category.AllowedIcons...)
	}
	color = strings.TrimSpace(color)
	if color == "" || slices.Contains(chartColors, color) || hexColor.MatchString(color) {
		return nil
	}
	return needs("color", color+" is not an accepted colour; use chart-1..chart-5 or #rrggbb", chartColors...)
}
