package handlers

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

// FirstRunHandler serves a project's first-run setup.
type FirstRunHandler struct {
	Deps
	Wallets      *wallet.Service
	Open         *wallet.OpenWorkflow
	TxCategories *category.Service
	Ledgers      *ledger.Service
}

// NewFirstRunHandler wires the handler.
func NewFirstRunHandler(
	d Deps,
	projects *project.Service,
	wallets *wallet.Service,
	open *wallet.OpenWorkflow,
	cats *category.Service,
	ledgers *ledger.Service,
) *FirstRunHandler {
	d.Projects = projects
	return &FirstRunHandler{Deps: d, Wallets: wallets, Open: open, TxCategories: cats, Ledgers: ledgers}
}

// Register wires the first-run routes onto mux.
func (h *FirstRunHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/first-run", h.Get)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/first-run", h.Post)
}

const maxFirstRunRows = 10

//nolint:gochecknoglobals // immutable lookup table
var regionCurrency = map[string]string{
	"ID": "IDR", "MY": "MYR", "SG": "SGD", "US": "USD", "JP": "JPY",
	"DE": "EUR", "FR": "EUR", "ES": "EUR", "IT": "EUR", "NL": "EUR",
	"BE": "EUR", "AT": "EUR", "IE": "EUR", "PT": "EUR", "FI": "EUR",
}

//nolint:gochecknoglobals // immutable preset list
var firstRunKinds = []wallet.Kind{wallet.KindCash, wallet.KindBank, wallet.KindEwallet, wallet.KindOther}

func firstRunPending(ctx context.Context, wallets *wallet.Service) (bool, error) {
	ws, err := wallets.List(ctx, wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		return false, err
	}
	return needsFirstRun(ws), nil
}

func needsFirstRun(ws []*wallet.Wallet) bool { return len(ws) == 0 }

func currencyForLocale(tag language.Tag, fallback money.Currency) money.Currency {
	r, _ := tag.Region()
	if c, ok := regionCurrency[r.String()]; ok {
		return money.Currency(c)
	}
	return fallback
}

// Get renders the current first-run step, or sends a project that has wallets home.
func (h *FirstRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if !h.stillPending(w, sc) {
		return
	}
	st, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		h.LogErr("web first-run: settings", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load project settings.", err)
		return
	}
	v := h.view(sc, st.Currency)
	if st.UpdatedAt.IsZero() {
		if !h.requireManager(w, sc) {
			return
		}
		v.Step = 1
		v.Currency = string(currencyForLocale(i18n.From(sc.req.Context()).Tag(), st.Currency))
	}
	h.render(w, sc, http.StatusOK, v)
}

// Post saves the currency or finishes the wallets step.
func (h *FirstRunHandler) Post(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	if sc.req.PostForm.Get("step") == "currency" {
		h.postCurrency(w, sc)
		return
	}
	h.postWallets(w, sc)
}

func (h *FirstRunHandler) postCurrency(w http.ResponseWriter, sc ProjectScope) {
	if !h.stillPending(w, sc) {
		return
	}
	cur := money.Currency(strings.TrimSpace(sc.req.PostForm.Get("currency")))
	if _, err := h.Ledgers.Update(sc.req.Context(), ledger.Patch{Currency: &cur}); err != nil {
		if h.refuseNotManager(w, sc.req, err, "firstrun.manager_only", "Only an owner or admin can set the project currency.") {
			return
		}
		h.LogErr("web first-run: currency", err)
		v := h.view(sc, cur)
		v.Step, v.ErrorKey, v.ErrorCode = 1, "firstrun.save_failed", ErrorRef(err)
		status := http.StatusInternalServerError
		if ledger.IsUnknownCurrencyError(err) {
			status, v.ErrorKey = http.StatusUnprocessableEntity, "firstrun.currency_invalid"
		}
		h.render(w, sc, status, v)
		return
	}
	http.Redirect(w, sc.req, h.firstRunURL(sc, "/first-run"), http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
}

func (h *FirstRunHandler) postWallets(w http.ResponseWriter, sc ProjectScope) {
	ctx := sc.req.Context()
	st, err := h.Ledgers.Get(ctx)
	if err != nil {
		h.LogErr("web first-run: settings", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load project settings.", err)
		return
	}
	if st.UpdatedAt.IsZero() {
		http.Redirect(w, sc.req, h.firstRunURL(sc, "/first-run"), http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
		return
	}
	if !h.stillPending(w, sc) {
		return
	}
	form := sc.req.PostForm
	v := h.view(sc, st.Currency)
	v.Rows = h.rowsFrom(form)
	if h.editRows(sc, form, &v) {
		h.render(w, sc, http.StatusOK, v)
		return
	}
	if len(v.Rows) == 0 {
		v.NoWallets = true
		h.render(w, sc, http.StatusUnprocessableEntity, v)
		return
	}
	tag := i18n.From(ctx).Tag()
	openings := make([]*money.Amount, len(v.Rows))
	invalid := false
	seen := map[string]bool{}
	for i := range v.Rows {
		row := &v.Rows[i]
		row.Name = strings.TrimSpace(row.Name)
		row.NameMissing = row.Name == ""
		folded := strings.ToLower(row.Name)
		row.NameDup = !row.NameMissing && seen[folded]
		seen[folded] = true
		invalid = invalid || row.NameMissing || row.NameDup
		if strings.TrimSpace(row.Amount) == "" {
			continue
		}
		a, pErr := parseAmount(row.Amount, st.Currency, tag)
		if pErr != nil {
			row.AmountErrKey = amountErrorKey(pErr)
			invalid = true
			continue
		}
		openings[i] = &a
	}
	if invalid {
		h.render(w, sc, http.StatusUnprocessableEntity, v)
		return
	}
	if _, err := h.TxCategories.SeedDefaults(ctx); err != nil {
		h.fail(w, sc, v, err)
		return
	}
	for i, row := range v.Rows {
		params := wallet.Params{Name: row.Name, Kind: wallet.Kind(row.Kind), Currency: st.Currency}
		if _, _, err := h.Open.Run(ctx, params, openings[i], time.Now().UTC()); err != nil && !wallet.IsAlreadyExistsError(err) {
			h.fail(w, sc, v, err)
			return
		}
	}
	h.redirectHome(w, sc)
}

func (h *FirstRunHandler) stillPending(w http.ResponseWriter, sc ProjectScope) bool {
	pending, err := firstRunPending(sc.req.Context(), h.Wallets)
	if err != nil {
		h.LogErr("web first-run: wallets", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load wallets.", err)
		return false
	}
	if !pending {
		h.redirectHome(w, sc)
	}
	return pending
}

// SECURITY: membership is org-wide and the currency is project-wide, so only an owner or admin may set it, and a failed lookup refuses.
//
//i18n:use firstrun.manager_only
//i18n:use firstrun.save_failed
//i18n:use firstrun.currency_invalid
func (h *FirstRunHandler) requireManager(w http.ResponseWriter, sc ProjectScope) bool {
	if h.CanManage(sc) {
		return true
	}
	h.ErrorPage(w, sc.req, http.StatusForbidden,
		localizedDefault(sc.req, "settings.name_forbidden_title", "Not allowed"),
		localizedDefault(sc.req, "firstrun.manager_only", "Only an owner or admin can set the project currency."))
	return false
}

func (h *FirstRunHandler) fail(w http.ResponseWriter, sc ProjectScope, v templates.FirstRunView, err error) {
	h.LogErr("web first-run: wallets", err)
	v.ErrorKey, v.ErrorCode = "firstrun.save_failed", ErrorRef(err)
	h.render(w, sc, http.StatusInternalServerError, v)
}

func (h *FirstRunHandler) rowsFrom(form map[string][]string) []templates.FirstRunRow {
	kinds, names, amounts := form["kind"], form["name"], form["amount"]
	var rows []templates.FirstRunRow
	for i, k := range kinds {
		kind, err := wallet.ParseKind(k)
		if err != nil || !slices.Contains(firstRunKinds, kind) || len(rows) == maxFirstRunRows {
			continue
		}
		row := templates.FirstRunRow{Kind: string(kind), KindKey: walletKindKey(kind)}
		if i < len(names) {
			row.Name = names[i]
		}
		if i < len(amounts) {
			row.Amount = amounts[i]
		}
		rows = append(rows, row)
	}
	return rows
}

func (h *FirstRunHandler) editRows(sc ProjectScope, form map[string][]string, v *templates.FirstRunView) bool {
	if k := first(form["add"]); k != "" {
		kind, err := wallet.ParseKind(k)
		if err == nil && slices.Contains(firstRunKinds, kind) && len(v.Rows) < maxFirstRunRows {
			v.Rows = append(v.Rows, templates.FirstRunRow{
				Kind: string(kind), KindKey: walletKindKey(kind), Name: h.defaultName(sc, kind, v.Rows),
			})
		}
		return true
	}
	if n, err := strconv.Atoi(first(form["remove"])); err == nil {
		if n >= 1 && n <= len(v.Rows) {
			v.Rows = append(v.Rows[:n-1], v.Rows[n:]...)
		}
		return true
	}
	return false
}

func (h *FirstRunHandler) defaultName(sc ProjectScope, kind wallet.Kind, rows []templates.FirstRunRow) string {
	base := i18n.TranslatorFrom(sc.req.Context()).T(walletKindKey(kind))
	taken := map[string]bool{}
	for _, r := range rows {
		taken[strings.ToLower(strings.TrimSpace(r.Name))] = true
	}
	name := base
	for n := 2; taken[strings.ToLower(name)]; n++ {
		name = base + " " + strconv.Itoa(n)
	}
	return name
}

func first(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func (h *FirstRunHandler) view(sc ProjectScope, cur money.Currency) templates.FirstRunView {
	v := templates.FirstRunView{
		ProjectSlug: sc.project.Slug, Step: 2, Currency: string(cur), Symbol: cur.SymbolFor(i18n.From(sc.req.Context()).Tag())}
	tr := i18n.TranslatorFrom(sc.req.Context()).T
	for _, c := range settingsCurrencies {
		v.Currencies = append(v.Currencies, templates.FirstRunCurrency{Code: c, Label: templates.CurrencyLabel(tr, money.Currency(c))})
	}
	for _, k := range firstRunKinds {
		v.Chips = append(v.Chips, templates.FirstRunChip{Kind: string(k), LabelKey: walletKindKey(k)})
	}
	return v
}

func (h *FirstRunHandler) render(w http.ResponseWriter, sc ProjectScope, status int, v templates.FirstRunView) {
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "title.first_run", sc.project.Name), sc.org.Slug, sc.project, "")
	RenderStatus(w, sc.req, status, templates.FirstRunLayout(d, v))
}

func (h *FirstRunHandler) firstRunURL(sc ProjectScope, suffix string) string {
	return web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, suffix))
}

func (h *FirstRunHandler) redirectHome(w http.ResponseWriter, sc ProjectScope) {
	http.Redirect(w, sc.req, h.firstRunURL(sc, "/overview"), http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
}
