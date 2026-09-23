package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const walletRecentLimit = 20

// WalletHandler owns the project-scoped wallet screens.
type WalletHandler struct {
	Deps
	Wallets      *wallet.Service
	Open         *wallet.OpenWorkflow
	Transactions *transaction.Service
	Ledgers      *ledger.Service
}

// NewWalletHandler wires the handler.
func NewWalletHandler(
	d Deps,
	projects *project.Service,
	wallets *wallet.Service,
	open *wallet.OpenWorkflow,
	txs *transaction.Service,
	ledgers *ledger.Service,
) *WalletHandler {
	d.Projects = projects
	return &WalletHandler{Deps: d, Wallets: wallets, Open: open, Transactions: txs, Ledgers: ledgers}
}

func requireYasakuProject(d Deps, w http.ResponseWriter, r *http.Request) (projectScope, bool) {
	p, sid, ok := d.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(d.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return projectScope{}, false
	}
	o, r, ok := d.OrgScopeFor(w, r, p, r.PathValue("org"))
	if !ok {
		return projectScope{}, false
	}
	proj, r, ok := d.ProjectScopeFor(w, r, o.ID, r.PathValue("project"))
	if !ok {
		return projectScope{}, false
	}
	return projectScope{principal: p, sid: sid, org: o, project: proj, req: r}, true
}

// GetWallets renders the wallets index.
func (h *WalletHandler) GetWallets(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	v, err := h.walletsView(sc, banner{})
	if err != nil {
		h.LogErr("web wallet: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load wallets.", err)
		return
	}
	Render(w, sc.req, templates.WalletsLayout(h.walletLayout(sc, "Wallets"), v))
}

// GetNew renders the empty wallet form.
func (h *WalletHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	currency, err := h.currency(sc)
	if err != nil {
		h.LogErr("web wallet: settings", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load project settings.", err)
		return
	}
	v := templates.WalletFormView{
		ProjectSlug: sc.project.Slug,
		Currency:    string(currency),
		Kinds:       walletKindOptions(wallet.KindCash),
	}
	Render(w, sc.req, templates.WalletFormLayout(h.walletLayout(sc, "New wallet"), v))
}

// PostCreate opens a wallet, recording any opening balance as its first transaction.
func (h *WalletHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	currency, err := h.currency(sc)
	if err != nil {
		h.LogErr("web wallet: settings", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load project settings.", err)
		return
	}

	form := sc.req.PostForm
	kind, kindErr := wallet.ParseKind(form.Get("kind"))
	v := templates.WalletFormView{
		ProjectSlug: sc.project.Slug,
		Name:        strings.TrimSpace(form.Get("name")),
		Provider:    strings.TrimSpace(form.Get("provider")),
		Currency:    string(currency),
		Opening:     strings.TrimSpace(form.Get("opening_balance")),
		Exclude:     form.Get("exclude_from_total") != "",
		Kinds:       walletKindOptions(kind),
	}
	if kindErr != nil {
		h.writeWalletForm(w, sc, v, bannerFrom(kindErr), "New wallet")
		return
	}

	var opening *money.Amount
	if v.Opening != "" {
		amount, pErr := parseAmount(v.Opening, currency)
		if pErr != nil {
			h.writeWalletForm(w, sc, v, bannerFrom(pErr), "New wallet")
			return
		}
		opening = &amount
	}

	params := wallet.Params{
		Name:             v.Name,
		Kind:             kind,
		Provider:         v.Provider,
		Currency:         currency,
		ExcludeFromTotal: v.Exclude,
	}
	if opening != nil {
		_, err = h.Open.Run(sc.req.Context(), params, opening, time.Now().UTC())
	} else {
		_, err = h.Wallets.Create(sc.req.Context(), params)
	}
	if err != nil {
		h.LogErr("web wallet: create", err)
		h.writeWalletForm(w, sc, v, bannerFrom(err), "New wallet")
		return
	}
	h.redirectToWallets(w, sc)
}

// GetDetail renders one wallet with its derived balance and most recent movements.
func (h *WalletHandler) GetDetail(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	h.writeWalletDetail(w, sc, wl, adjustState(sc.req.URL.Query().Get("adjust")), banner{})
}

// GetEdit renders the wallet form prefilled from the row.
func (h *WalletHandler) GetEdit(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	b := banner{}
	if wl.IsArchived() {
		b = bannerFrom(&wallet.ArchivedError{ID: wl.ID.String()})
	}
	h.writeWalletForm(w, sc, walletFormFrom(sc, wl), b, "Edit wallet")
}

// PostUpdate replaces a wallet's name, kind, provider and exclude-from-total flag in one save.
func (h *WalletHandler) PostUpdate(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	form := sc.req.PostForm
	kind, kindErr := wallet.ParseKind(form.Get("kind"))
	v := walletFormFrom(sc, wl)
	v.Name = strings.TrimSpace(form.Get("name"))
	v.Provider = strings.TrimSpace(form.Get("provider"))
	v.Exclude = form.Get("exclude_from_total") != ""
	v.Kinds = walletKindOptions(kind)
	if kindErr != nil {
		h.writeWalletForm(w, sc, v, bannerFrom(kindErr), "Edit wallet")
		return
	}
	if _, err := h.Wallets.Edit(sc.req.Context(), wl.ID, v.Name, kind, v.Provider, v.Exclude); err != nil {
		h.LogErr("web wallet: edit", err)
		h.writeWalletForm(w, sc, v, bannerFrom(err), "Edit wallet")
		return
	}
	h.redirectToWallets(w, sc)
}

// PostArchive retires a wallet and returns the refreshed list fragment.
func (h *WalletHandler) PostArchive(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	if _, err := h.Wallets.Archive(sc.req.Context(), wl.ID); err != nil {
		h.LogErr("web wallet: archive", err)
		h.writeWalletList(w, sc, bannerFrom(err))
		return
	}
	h.writeWalletList(w, sc, banner{})
}

// PostUnarchive returns a wallet to active use and returns the refreshed list fragment.
func (h *WalletHandler) PostUnarchive(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	if _, err := h.Wallets.Unarchive(sc.req.Context(), wl.ID); err != nil {
		h.LogErr("web wallet: unarchive", err)
		h.writeWalletList(w, sc, bannerFrom(err))
		return
	}
	h.writeWalletList(w, sc, banner{})
}

// PostDelete removes a wallet; one that still holds transactions comes back as a banner offering Archive.
func (h *WalletHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	if err := h.Wallets.Delete(sc.req.Context(), wl.ID); err != nil {
		h.LogErr("web wallet: delete", err)
		h.writeWalletList(w, sc, bannerFrom(err))
		return
	}
	h.writeWalletList(w, sc, banner{})
}

// GetAdjust renders the balance-adjustment form for one wallet.
func (h *WalletHandler) GetAdjust(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	b := banner{}
	if wl.IsArchived() {
		b = bannerFrom(&wallet.ArchivedError{ID: wl.ID.String()})
	}
	h.writeAdjustForm(w, sc, wl, "", b)
}

// PostAdjust brings a wallet's derived balance to the target; an unchanged balance writes nothing.
func (h *WalletHandler) PostAdjust(w http.ResponseWriter, r *http.Request) {
	sc, wl, ok := h.requireWallet(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	raw := strings.TrimSpace(sc.req.PostForm.Get("target"))
	target, err := parseAmount(raw, wl.Currency)
	if err != nil {
		h.writeAdjustForm(w, sc, wl, raw, bannerFrom(err))
		return
	}
	written, err := h.Transactions.Adjust(sc.req.Context(), wl.ID, target, time.Now().UTC(), sc.principal.UserID)
	if err != nil {
		h.LogErr("web wallet: adjust", err)
		h.writeAdjustForm(w, sc, wl, raw, bannerFrom(err))
		return
	}
	state := templates.AdjustUnchanged
	if written != nil {
		state = templates.AdjustChanged
	}
	h.redirectToWallet(w, sc, wl.ID, state)
}

func adjustState(q string) string {
	if q == templates.AdjustChanged || q == templates.AdjustUnchanged {
		return q
	}
	return ""
}

// Register wires the wallet routes onto mux.
func (h *WalletHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/wallets", h.GetWallets)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/wallets/new", h.GetNew)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets", h.PostCreate)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/wallets/{id}", h.GetDetail)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/wallets/{id}/edit", h.GetEdit)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets/{id}", h.PostUpdate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets/{id}/archive", h.PostArchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets/{id}/unarchive", h.PostUnarchive)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets/{id}/delete", h.PostDelete)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/wallets/{id}/adjust", h.GetAdjust)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/wallets/{id}/adjust", h.PostAdjust)
}

type banner struct {
	Key  string
	Msg  string
	Code string
}

func bannerFrom(err error) banner {
	if err == nil {
		return banner{}
	}
	out := banner{Code: ErrorRef(err)}
	if wallet.IsInUseError(err) {
		out.Key = "wallet.in_use"
		return out
	}
	if ae, ok := apperror.AsAppError(err); ok {
		out.Msg = ae.Message()
		return out
	}
	out.Msg = err.Error()
	return out
}

func parseAmount(raw string, cur money.Currency) (money.Amount, error) {
	a, err := money.ParseMajor(raw, cur)
	if err == nil {
		return a, nil
	}
	if pe, ok := errors.AsType[*money.ParseError](err); ok {
		return money.Amount{}, &transaction.InvalidAmountError{Reason: pe.Reason}
	}
	return money.Amount{}, err
}

func (h *WalletHandler) requireWallet(w http.ResponseWriter, r *http.Request) (projectScope, *wallet.Wallet, bool) {
	sc, ok := requireYasakuProject(h.Deps, w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed wallet id.")
		return projectScope{}, nil, false
	}
	wl, err := h.Wallets.ByID(sc.req.Context(), id)
	if err != nil {
		if wallet.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That wallet no longer exists.", err)
			return projectScope{}, nil, false
		}
		h.LogErr("web wallet: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that wallet.", err)
		return projectScope{}, nil, false
	}
	return sc, wl, true
}

func (h *WalletHandler) walletLayout(sc projectScope, title string) web.LayoutData {
	return h.LayoutForProject(sc.req, title+" · "+sc.project.Name, sc.org.Slug, sc.project, "wallets")
}

func (h *WalletHandler) currency(sc projectScope) (money.Currency, error) {
	settings, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		return "", err
	}
	return settings.Currency, nil
}

func (h *WalletHandler) walletsView(sc projectScope, b banner) (templates.WalletsView, error) {
	ctx := sc.req.Context()
	items, err := h.Wallets.List(ctx, wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		return templates.WalletsView{}, err
	}
	balances, err := h.Transactions.Balances(ctx)
	if err != nil {
		return templates.WalletsView{}, err
	}
	currency, err := h.currency(sc)
	if err != nil {
		return templates.WalletsView{}, err
	}

	v := templates.WalletsView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Total:       money.Zero(currency),
		ErrorKey:    b.Key,
		ErrorMsg:    b.Msg,
		ErrorCode:   b.Code,
	}
	for _, it := range items {
		row := walletRow(it, balanceOf(balances, it))
		if it.IsArchived() {
			v.Archived = append(v.Archived, row)
			continue
		}
		v.Active = append(v.Active, row)
		if !it.ExcludeFromTotal && row.Balance.Currency == v.Total.Currency {
			v.Total = v.Total.Add(row.Balance)
		}
	}
	return v, nil
}

// balanceOf reads w's derived balance. NOTE: Balances only carries wallets with at least one
// transaction, so an absent wallet is a zero in its own currency, never a currency-less money.Amount.
func balanceOf(balances map[uuid.UUID]money.Amount, w *wallet.Wallet) money.Amount {
	bal, ok := balances[w.ID]
	if !ok || bal.Currency == "" {
		return money.Zero(w.Currency)
	}
	return bal
}

func walletRow(w *wallet.Wallet, balance money.Amount) templates.WalletRow {
	return templates.WalletRow{
		ID:       w.ID.String(),
		Name:     w.Name,
		KindKey:  walletKindKey(w.Kind),
		Provider: w.Provider,
		Balance:  balance,
		Excluded: w.ExcludeFromTotal,
		Archived: w.IsArchived(),
	}
}

func walletFormFrom(sc projectScope, w *wallet.Wallet) templates.WalletFormView {
	return templates.WalletFormView{
		ProjectSlug: sc.project.Slug,
		ID:          w.ID.String(),
		Name:        w.Name,
		Provider:    w.Provider,
		Currency:    string(w.Currency),
		Exclude:     w.ExcludeFromTotal,
		Kinds:       walletKindOptions(w.Kind),
	}
}

func (h *WalletHandler) writeWalletList(w http.ResponseWriter, sc projectScope, b banner) {
	v, err := h.walletsView(sc, b)
	if err != nil {
		h.LogErr("web wallet: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load wallets.", err)
		return
	}
	Render(w, sc.req, templates.WalletList(h.walletLayout(sc, "Wallets"), v))
}

func (h *WalletHandler) writeWalletForm(w http.ResponseWriter, sc projectScope, v templates.WalletFormView, b banner, title string) {
	v.ErrorKey, v.ErrorMsg, v.ErrorCode = b.Key, b.Msg, b.Code
	Render(w, sc.req, templates.WalletFormLayout(h.walletLayout(sc, title), v))
}

func (h *WalletHandler) writeWalletDetail(w http.ResponseWriter, sc projectScope, wl *wallet.Wallet, state string, b banner) {
	ctx := sc.req.Context()
	balance, err := h.Transactions.Balance(ctx, wl.ID)
	if err != nil {
		h.LogErr("web wallet: balance", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load that wallet.", err)
		return
	}
	items, _, err := h.Transactions.List(ctx, transaction.ListOpts{WalletID: &wl.ID, Limit: walletRecentLimit})
	if err != nil {
		h.LogErr("web wallet: list transactions", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load that wallet.", err)
		return
	}
	v := templates.WalletDetailView{
		ProjectSlug: sc.project.Slug,
		Wallet:      walletRow(wl, balance),
		Balance:     balance,
		Recent:      walletTxRows(wl.ID, items, h.location(sc)),
		AdjustState: state,
		ErrorKey:    b.Key,
		ErrorMsg:    b.Msg,
		ErrorCode:   b.Code,
	}
	Render(w, sc.req, templates.WalletDetailLayout(h.walletLayout(sc, wl.Name), v))
}

func (h *WalletHandler) writeAdjustForm(w http.ResponseWriter, sc projectScope, wl *wallet.Wallet, target string, b banner) {
	balance, err := h.Transactions.Balance(sc.req.Context(), wl.ID)
	if err != nil {
		h.LogErr("web wallet: balance", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load that wallet.", err)
		return
	}
	v := templates.WalletAdjustView{
		ProjectSlug: sc.project.Slug,
		WalletID:    wl.ID.String(),
		WalletName:  wl.Name,
		Current:     balance,
		Target:      target,
		ErrorKey:    b.Key,
		ErrorMsg:    b.Msg,
		ErrorCode:   b.Code,
	}
	Render(w, sc.req, templates.WalletAdjustLayout(h.walletLayout(sc, "Adjust balance"), v))
}

func (h *WalletHandler) redirectToWallet(w http.ResponseWriter, sc projectScope, id uuid.UUID, state string) {
	target := web.Path(h.Cfg.HTTP.BasePath,
		projectPath(sc.org.Slug, sc.project.Slug, "/wallets/"+id.String())) + "?adjust=" + state
	http.Redirect(w, sc.req, target, http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
}

func (h *WalletHandler) redirectToWallets(w http.ResponseWriter, sc projectScope) {
	target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/wallets"))
	http.Redirect(w, sc.req, target, http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
}

func walletTxRows(walletID uuid.UUID, items []*transaction.Transaction, loc *time.Location) []templates.WalletTxRow {
	rows := make([]templates.WalletTxRow, 0, len(items))
	for _, t := range items {
		rows = append(rows, templates.WalletTxRow{
			KindKey: transactionKindKey(t.Kind),
			Signed:  signedFor(walletID, t),
			Note:    t.Note,
			Date:    civil.DateOf(t.OccurredAt, loc).String(),
		})
	}
	return rows
}

func (h *WalletHandler) location(sc projectScope) *time.Location {
	s, err := h.Ledgers.Get(sc.req.Context())
	if err != nil || s == nil {
		return time.UTC
	}
	loc, err := s.Location()
	if err != nil {
		return time.UTC
	}
	return loc
}

func signedFor(walletID uuid.UUID, t *transaction.Transaction) money.Amount {
	if t.Kind == transaction.KindTransfer && t.ToWalletID != nil && *t.ToWalletID == walletID {
		return t.Amount
	}
	if t.Kind.IsInflow() {
		return t.Amount
	}
	return t.Amount.Neg()
}

func walletKindKey(k wallet.Kind) string {
	return "wallet.kind_" + string(k)
}

func walletKindOptions(selected wallet.Kind) []templates.WalletKindOption {
	kinds := []wallet.Kind{
		wallet.KindCash, wallet.KindBank, wallet.KindEwallet,
		wallet.KindSavings, wallet.KindInvestment, wallet.KindOther,
	}
	out := make([]templates.WalletKindOption, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, templates.WalletKindOption{
			Value:    string(k),
			LabelKey: walletKindKey(k),
			Selected: k == selected,
		})
	}
	return out
}
