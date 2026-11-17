package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// TxCategoryHandler owns the project-scoped transaction-categories screen, apart from BlogHandler's blog categories.
type TxCategoryHandler struct {
	Deps
	TxCategories *category.Service
	Periods      *period.Service
	Reports      *report.Service
}

// NewTxCategoryHandler wires the handler.
func NewTxCategoryHandler(d Deps, projects *project.Service, cats *category.Service, periods *period.Service, reports *report.Service) *TxCategoryHandler {
	d.Projects = projects
	return &TxCategoryHandler{Deps: d, TxCategories: cats, Periods: periods, Reports: reports}
}

// GetCategories renders the transaction-categories page.
func (h *TxCategoryHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	kind, err := category.ParseKind(sc.req.URL.Query().Get("kind"))
	if err != nil {
		kind = category.KindExpense
	}
	v, err := h.categoriesView(sc, banner{}, -1, kind)
	if err != nil {
		h.LogErr("web category: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load categories.", err)
		return
	}
	if web.IsHTMXRequest(sc.req) {
		v.Fragment = true
		Render(w, sc.req, templates.TxCategoryList(h.txCategoryLayout(sc), v))
		return
	}
	Render(w, sc.req, templates.TxCategoriesLayout(h.txCategoryLayout(sc), v))
}

// PostCreate adds a category and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	kind, err := category.ParseKind(sc.req.PostForm.Get("kind"))
	if err != nil {
		h.refuseCreate(w, sc, category.KindExpense, err)
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.TxCategories.Create(sc.req.Context(), name, kind, "", ""); err != nil {
		h.LogErr("web category: create", err)
		h.refuseCreate(w, sc, kind, err)
		return
	}
	h.writeTxCategoryList(w, sc, banner{}, kind)
	if web.IsHTMXRequest(sc.req) {
		Render(w, sc.req, templates.TxCategoryFormErrors(h.txCategoryLayout(sc), templates.TxCategoriesView{}, true))
	}
}

func (h *TxCategoryHandler) refuseCreate(w http.ResponseWriter, sc ProjectScope, kind category.Kind, err error) {
	if !web.IsHTMXRequest(sc.req) {
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), kind)
		return
	}
	b := categoryBannerFrom(err)
	v := templates.TxCategoriesView{ErrorKey: b.Key, ErrorMsg: b.Msg, ErrorCode: b.Code}
	RenderStatus(w, sc.req, http.StatusUnprocessableEntity, templates.TxCategoryFormErrors(h.txCategoryLayout(sc), v, false))
}

// PostSeed inserts every missing default category and reports how many it added.
func (h *TxCategoryHandler) PostSeed(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	added, err := h.TxCategories.SeedDefaults(sc.req.Context())
	if err != nil {
		h.LogErr("web category: seed", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), category.KindExpense)
		return
	}
	h.writeTxCategoryListSeeded(w, sc, banner{}, added, category.KindExpense)
}

// PostRename renames a category and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostRename(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.TxCategories.Rename(sc.req.Context(), c.ID, name); err != nil {
		h.LogErr("web category: rename", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), c.Kind)
		return
	}
	h.writeTxCategoryList(w, sc, banner{}, c.Kind)
}

// PostArchive hides a category from the pickers and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostArchive(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if _, err := h.TxCategories.Archive(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: archive", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), c.Kind)
		return
	}
	h.listWritten(w, sc, c.Kind)
}

// PostUnarchive returns a category to the pickers and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostUnarchive(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if _, err := h.TxCategories.Unarchive(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: unarchive", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), c.Kind)
		return
	}
	h.listWritten(w, sc, c.Kind)
}

// PostDelete removes a category; one still on transactions comes back as a banner offering Archive.
func (h *TxCategoryHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := h.TxCategories.Delete(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: delete", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), c.Kind)
		return
	}
	h.listWritten(w, sc, c.Kind)
}

// PostIcon changes a category's icon, keeping its colour, and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostIcon(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	if _, err := h.TxCategories.Update(sc.req.Context(), c.ID, strings.TrimSpace(sc.req.PostForm.Get("icon")), c.Color); err != nil {
		h.LogErr("web category: icon", err)
		h.writeTxCategoryList(w, sc, categoryBannerFrom(err), c.Kind)
		return
	}
	h.writeTxCategoryList(w, sc, banner{}, c.Kind)
}

// Register wires the transaction-category routes onto mux.
func (h *TxCategoryHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/categories", h.GetCategories)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories", h.PostCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/seed", h.PostSeed)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/rename", h.PostRename)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/icon", h.PostIcon)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/archive", h.PostArchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/unarchive", h.PostUnarchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/delete", h.PostDelete)
}

//i18n:use category.in_use
func categoryBannerFrom(err error) banner {
	if category.IsInUseError(err) {
		return banner{Key: "category.in_use", Code: ErrorRef(err)}
	}
	return bannerFrom(err)
}

func (h *TxCategoryHandler) requireCategory(w http.ResponseWriter, r *http.Request) (ProjectScope, *category.Category, bool) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return ProjectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_id"), "Malformed category id.")
		return ProjectScope{}, nil, false
	}
	c, err := h.TxCategories.ByID(sc.req.Context(), id)
	if err != nil {
		if category.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_found"), "That category no longer exists.", err)
			return ProjectScope{}, nil, false
		}
		h.LogErr("web category: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.lookup_failed"), "Could not load that category.", err)
		return ProjectScope{}, nil, false
	}
	return sc, c, true
}

func (h *TxCategoryHandler) txCategoryLayout(sc ProjectScope) web.LayoutData {
	return h.LayoutForProject(sc.req, pageTitle(sc.req, "nav.categories", sc.project.Name), sc.org.Slug, sc.project, "categories")
}

//i18n:use category.icon.*
//i18n:use category.archive
//i18n:use category.unarchive
//i18n:use tx.kind_*
func (h *TxCategoryHandler) categoriesView(sc ProjectScope, b banner, seeded int, kind category.Kind) (templates.TxCategoriesView, error) {
	rows, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{IncludeArchived: true})
	if err != nil {
		return templates.TxCategoriesView{}, err
	}
	v := templates.TxCategoriesView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Tab:         string(kind),
		AnyCategory: len(rows) > 0,
		Seeded:      seeded,
		ShowSeeded:  seeded >= 0,
		ErrorKey:    b.Key,
		ErrorMsg:    b.Msg,
		ErrorCode:   b.Code,
	}
	counts, haveCounts := h.entriesThisPeriod(sc, kind)
	for _, c := range rows {
		if c.Kind != kind {
			continue
		}
		row := templates.TxCategoryRow{
			ID:       c.ID.String(),
			Name:     c.Name,
			KindKey:  txCategoryKindKey(c.Kind),
			Icon:     c.Icon,
			Archived: c.IsArchived(),
		}
		if row.Icon == "" {
			row.Icon = "circle-ellipsis"
		}
		row.Entries, row.HasEntries = counts[c.ID], haveCounts
		if row.Archived {
			v.Archived = append(v.Archived, row)
			continue
		}
		v.Rows = append(v.Rows, row)
	}
	return v, nil
}

// NOTE: counts reuse the period report's category slices; a missing period or a failed report hides the counts instead of showing zeros.
func (h *TxCategoryHandler) entriesThisPeriod(sc ProjectScope, kind category.Kind) (map[uuid.UUID]int, bool) {
	ctx := sc.req.Context()
	cur, err := h.Periods.Current(ctx)
	if err != nil {
		if !period.IsNotFoundError(err) {
			h.LogErr("web category: current period", err)
		}
		return nil, false
	}
	slices, err := h.Reports.SpendByCategory(ctx, cur.ID)
	if kind == category.KindIncome {
		slices, err = h.Reports.IncomeByCategory(ctx, cur.ID)
	}
	if err != nil {
		h.LogErr("web category: period slices", err)
		return nil, false
	}
	counts := make(map[uuid.UUID]int, len(slices))
	for _, sl := range slices {
		if sl.CategoryID != nil {
			counts[*sl.CategoryID] = sl.Count
		}
	}
	return counts, true
}

// NOTE: a plain form post gets a 303 so a refresh cannot repeat the write; htmx swaps the fresh list in place.
func (h *TxCategoryHandler) listWritten(w http.ResponseWriter, sc ProjectScope, kind category.Kind) {
	if !web.IsHTMXRequest(sc.req) {
		target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/categories?kind="+string(kind)))
		http.Redirect(w, sc.req, target, http.StatusSeeOther) //nolint:gosec // G710: both slugs come from resolved rows and kind is a parsed enum
		return
	}
	h.writeTxCategoryList(w, sc, banner{}, kind)
}

func (h *TxCategoryHandler) writeTxCategoryList(w http.ResponseWriter, sc ProjectScope, b banner, kind category.Kind) {
	h.writeTxCategoryListSeeded(w, sc, b, -1, kind)
}

func (h *TxCategoryHandler) writeTxCategoryListSeeded(w http.ResponseWriter, sc ProjectScope, b banner, seeded int, kind category.Kind) {
	v, err := h.categoriesView(sc, b, seeded, kind)
	if err != nil {
		h.LogErr("web category: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load categories.", err)
		return
	}
	v.Fragment = web.IsHTMXRequest(sc.req)
	Render(w, sc.req, templates.TxCategoryList(h.txCategoryLayout(sc), v))
}

func txCategoryKindKey(k category.Kind) string {
	if k == category.KindIncome {
		return "tx.kind_income"
	}
	return "tx.kind_expense"
}
