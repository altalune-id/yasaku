package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// TxCategoryHandler owns the project-scoped transaction-categories screen.
// NOTE: named apart from BlogHandler's category methods, which serve the blog's own categories.
type TxCategoryHandler struct {
	Deps
	TxCategories *category.Service
}

// NewTxCategoryHandler wires the handler.
func NewTxCategoryHandler(d Deps, projects *project.Service, cats *category.Service) *TxCategoryHandler {
	d.Projects = projects
	return &TxCategoryHandler{Deps: d, TxCategories: cats}
}

// GetCategories renders the transaction-categories page.
func (h *TxCategoryHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	v, err := h.categoriesView(sc, walletError{}, -1)
	if err != nil {
		h.LogErr("web category: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load categories.", err)
		return
	}
	Render(w, sc.req, templates.TxCategoriesLayout(h.txCategoryLayout(sc), v))
}

// PostCreate adds a category and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	kind, err := category.ParseKind(sc.req.PostForm.Get("kind"))
	if err != nil {
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.TxCategories.Create(sc.req.Context(), name, kind, "", ""); err != nil {
		h.LogErr("web category: create", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryList(w, sc, walletError{})
}

// PostSeed inserts every missing default category and reports how many it added.
func (h *TxCategoryHandler) PostSeed(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	added, err := h.TxCategories.SeedDefaults(sc.req.Context())
	if err != nil {
		h.LogErr("web category: seed", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryListSeeded(w, sc, walletError{}, added)
}

// PostRename renames a category and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostRename(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.TxCategories.Rename(sc.req.Context(), c.ID, name); err != nil {
		h.LogErr("web category: rename", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryList(w, sc, walletError{})
}

// PostArchive hides a category from the pickers and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostArchive(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if _, err := h.TxCategories.Archive(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: archive", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryList(w, sc, walletError{})
}

// PostUnarchive returns a category to the pickers and returns the refreshed list fragment.
func (h *TxCategoryHandler) PostUnarchive(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if _, err := h.TxCategories.Unarchive(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: unarchive", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryList(w, sc, walletError{})
}

// PostDelete removes a category; one still on transactions comes back as a banner offering Archive.
func (h *TxCategoryHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := h.TxCategories.Delete(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web category: delete", err)
		h.writeTxCategoryList(w, sc, txCategoryErrorFrom(err))
		return
	}
	h.writeTxCategoryList(w, sc, walletError{})
}

// Register wires the transaction-category routes onto mux.
func (h *TxCategoryHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/categories", h.GetCategories)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories", h.PostCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/seed", h.PostSeed)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/rename", h.PostRename)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/archive", h.PostArchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/unarchive", h.PostUnarchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/delete", h.PostDelete)
}

func txCategoryErrorFrom(err error) walletError {
	if category.IsInUseError(err) {
		return walletError{Key: "category.in_use", Code: ErrorRef(err)}
	}
	return walletErrorFrom(err)
}

func (h *TxCategoryHandler) requireCategory(w http.ResponseWriter, r *http.Request) (projectScope, *category.Category, bool) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed category id.")
		return projectScope{}, nil, false
	}
	c, err := h.TxCategories.ByID(sc.req.Context(), id)
	if err != nil {
		if category.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That category no longer exists.", err)
			return projectScope{}, nil, false
		}
		h.LogErr("web category: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that category.", err)
		return projectScope{}, nil, false
	}
	return sc, c, true
}

// txCategoryLayout builds the full project layout; fragments use it too, so their action URLs keep the org scope.
func (h *TxCategoryHandler) txCategoryLayout(sc projectScope) web.LayoutData {
	return h.LayoutForProject(sc.req, "Categories · "+sc.project.Name, sc.org.Slug, sc.project, "categories")
}

func (h *TxCategoryHandler) categoriesView(sc projectScope, banner walletError, seeded int) (templates.TxCategoriesView, error) {
	rows, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{IncludeArchived: true})
	if err != nil {
		return templates.TxCategoriesView{}, err
	}
	v := templates.TxCategoriesView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Seeded:      seeded,
		ShowSeeded:  seeded >= 0,
		ErrorKey:    banner.Key,
		ErrorMsg:    banner.Msg,
		ErrorCode:   banner.Code,
	}
	for _, c := range rows {
		row := templates.TxCategoryRow{
			ID:       c.ID.String(),
			Name:     c.Name,
			KindKey:  txCategoryKindKey(c.Kind),
			Archived: c.IsArchived(),
		}
		switch {
		case row.Archived:
			v.Archived = append(v.Archived, row)
		case c.Kind == category.KindIncome:
			v.Income = append(v.Income, row)
		default:
			v.Expense = append(v.Expense, row)
		}
	}
	return v, nil
}

func (h *TxCategoryHandler) writeTxCategoryList(w http.ResponseWriter, sc projectScope, banner walletError) {
	h.writeTxCategoryListSeeded(w, sc, banner, -1)
}

func (h *TxCategoryHandler) writeTxCategoryListSeeded(w http.ResponseWriter, sc projectScope, banner walletError, seeded int) {
	v, err := h.categoriesView(sc, banner, seeded)
	if err != nil {
		h.LogErr("web category: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load categories.", err)
		return
	}
	Render(w, sc.req, templates.TxCategoryList(h.txCategoryLayout(sc), v))
}

func txCategoryKindKey(k category.Kind) string {
	if k == category.KindIncome {
		return "tx.kind_income"
	}
	return "tx.kind_expense"
}
