package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

// LastWalletCookie remembers the wallet a quick-add last used, so the next one opens on it.
const LastWalletCookie = "yasaku_last_wallet"

const (
	lastWalletMaxAge = 365 * 24 * 60 * 60
	txPageSize       = 50
	txRecentLimit    = 10
	txPeriodFilters  = 12
)

var errBadForm = errors.New("web transaction: malformed form field")

//nolint:gochecknoglobals // a fixed key list, not runtime state.
var txFilterKeys = []string{"wallet", "category", "period", "kind", "q"}

// SECURITY: opening and the adjustments are written by the service alone; a posted kind outside this set is refused.
//
//nolint:gochecknoglobals // a fixed kind list, not runtime state.
var txUserKinds = []transaction.Kind{transaction.KindExpense, transaction.KindIncome, transaction.KindTransfer}

// TransactionHandler owns the project-scoped transaction list, quick-add and edit screens.
type TransactionHandler struct {
	Deps
	Wallets      *wallet.Service
	Transactions *transaction.Service
	Periods      *period.Service
	TxCategories *category.Service
	Ledgers      *ledger.Service
}

// NewTransactionHandler wires the handler.
func NewTransactionHandler(
	d Deps,
	projects *project.Service,
	wallets *wallet.Service,
	transactions *transaction.Service,
	periods *period.Service,
	categories *category.Service,
	ledgers *ledger.Service,
) *TransactionHandler {
	d.Projects = projects
	return &TransactionHandler{
		Deps:         d,
		Wallets:      wallets,
		Transactions: transactions,
		Periods:      periods,
		TxCategories: categories,
		Ledgers:      ledgers,
	}
}

// Register wires the transaction routes onto mux.
func (h *TransactionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions", h.GetList)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions/new", h.GetNew)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions", h.PostCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/suggest", h.PostSuggest)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions/{id}/edit", h.GetEdit)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/{id}", h.PostUpdate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/{id}/delete", h.PostDelete)
}

func (h *TransactionHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
	p, sid, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return projectScope{}, false
	}
	o, r, ok := h.OrgScopeFor(w, r, p, r.PathValue("org"))
	if !ok {
		return projectScope{}, false
	}
	proj, r, ok := h.ProjectScopeFor(w, r, o.ID, r.PathValue("project"))
	if !ok {
		return projectScope{}, false
	}
	return projectScope{principal: p, sid: sid, org: o, project: proj, req: r}, true
}

// GetList renders the transactions page, or just the list for an HTMX filter or load-more request.
func (h *TransactionHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, h.title(sc, "tx.title"), sc.org.Slug, sc.project, "transactions")
	q := sc.req.URL.Query()

	list, err := h.listView(sc, d, q, txPageSize)
	if err != nil {
		h.LogErr("web transaction: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load transactions.", err)
		return
	}
	if q.Get("after") != "" {
		Render(w, sc.req, templates.TxRowsPage(d, list))
		return
	}
	if h.isHTMX(sc.req) {
		Render(w, sc.req, templates.TransactionList(d, list))
		return
	}

	form, err := h.formView(sc, d, txInput{Kind: transaction.KindExpense}, "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load transactions.", err)
		return
	}
	v := templates.TransactionsView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Form:        form,
		List:        list,
		Search:      q.Get("q"),
	}
	if err := h.fillFilters(sc, d, &v, q); err != nil {
		h.LogErr("web transaction: filters", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load transactions.", err)
		return
	}
	Render(w, sc.req, templates.TransactionsLayout(d, v))
}

// GetNew renders the standalone quick-add page, amount first.
func (h *TransactionHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, h.title(sc, "tx.new"), sc.org.Slug, sc.project, "transactions")
	form, err := h.formView(sc, d, txInput{Kind: transaction.KindExpense}, "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load the form.", err)
		return
	}
	Render(w, sc.req, templates.TransactionFormLayout(d, form, d.Tr("tx.new")))
}

// GetEdit renders the edit form for one transaction.
func (h *TransactionHandler) GetEdit(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, h.title(sc, "tx.edit"), sc.org.Slug, sc.project, "transactions")
	in := txInput{
		Kind:       t.Kind,
		Amount:     t.Amount.Major(),
		WalletID:   t.WalletID,
		ToWalletID: t.ToWalletID,
		CategoryID: t.CategoryID,
		PeriodID:   t.PeriodID,
		Date:       civil.DateOf(t.OccurredAt, h.location(sc)),
		Note:       t.Note,
	}
	form, err := h.formView(sc, d, in, t.ID.String())
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load that transaction.", err)
		return
	}
	Render(w, sc.req, templates.TransactionFormLayout(d, form, d.Tr("tx.edit")))
}

// PostCreate records one transaction and returns the refreshed list plus an out-of-band toast.
func (h *TransactionHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	in, amount, ok := h.readForm(w, sc, "")
	if !ok {
		return
	}
	t, err := h.Transactions.Record(sc.req.Context(), transaction.RecordInput{
		WalletID:   in.WalletID,
		ToWalletID: in.ToWalletID,
		Kind:       in.Kind,
		Amount:     amount,
		CategoryID: in.CategoryID,
		PeriodID:   in.PeriodID,
		Note:       in.Note,
		OccurredAt: h.occurredAt(sc, in),
	})
	if err != nil {
		h.LogErr("web transaction: record", err)
		h.refuse(w, sc, err)
		return
	}
	h.rememberWallet(w, t.WalletID)
	h.succeed(w, sc, t)
}

// PostUpdate revises one transaction and returns the refreshed list plus an out-of-band toast.
func (h *TransactionHandler) PostUpdate(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	in, amount, ok := h.readForm(w, sc, t.Kind)
	if !ok {
		return
	}
	at := h.occurredAt(sc, in)
	updated, err := h.Transactions.Revise(sc.req.Context(), t.ID, transaction.RevisePatch{
		WalletID:   &in.WalletID,
		ToWalletID: &in.ToWalletID,
		Amount:     &amount,
		CategoryID: &in.CategoryID,
		PeriodID:   &in.PeriodID,
		Note:       &in.Note,
		OccurredAt: &at,
	})
	if err != nil {
		h.LogErr("web transaction: revise", err)
		h.refuse(w, sc, err)
		return
	}
	h.rememberWallet(w, updated.WalletID)
	h.succeed(w, sc, updated)
}

// PostDelete removes one transaction, refusing with a banner when its period is closed.
func (h *TransactionHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	if err := h.Transactions.Delete(sc.req.Context(), t.ID); err != nil {
		h.LogErr("web transaction: delete", err)
		h.refuse(w, sc, err)
		return
	}
	h.succeed(w, sc, nil)
}

// PostSuggest returns the category chips with the category last used for this note preselected.
func (h *TransactionHandler) PostSuggest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	kind, err := parseUserKind(sc.req.PostForm.Get("kind"))
	if err != nil {
		kind = transaction.KindExpense
	}
	in := txInput{Kind: kind}
	if id, cErr := uuid.Parse(sc.req.PostForm.Get("category_id")); cErr == nil {
		in.CategoryID = &id
	}
	suggested, found, err := h.Transactions.SuggestCategory(sc.req.Context(), sc.req.PostForm.Get("note"))
	switch {
	case err != nil:
		h.LogErr("web transaction: suggest", err)
	case found:
		in.CategoryID = &suggested
		in.Suggested = &suggested
	}

	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "transactions")
	form, err := h.formView(sc, d, in, "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load categories.", err)
		return
	}
	Render(w, sc.req, templates.TxCategoryChips(d, form))
}

type txInput struct {
	Kind       transaction.Kind
	Amount     string
	WalletID   uuid.UUID
	ToWalletID *uuid.UUID
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Suggested  *uuid.UUID
	Date       civil.Date
	Note       string
}

func (h *TransactionHandler) readForm(w http.ResponseWriter, sc projectScope, want transaction.Kind) (txInput, money.Amount, bool) {
	in, err := h.parseForm(sc, want)
	if err != nil {
		if errors.Is(err, errBadForm) {
			h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not read that form.")
			return txInput{}, money.Amount{}, false
		}
		h.refuse(w, sc, err)
		return txInput{}, money.Amount{}, false
	}
	amount, err := h.amountFor(sc, in)
	if err != nil {
		h.refuse(w, sc, err)
		return txInput{}, money.Amount{}, false
	}
	return in, amount, true
}

// SECURITY: want pins the kind the stored row already has, so a posted kind can never switch it.
func (h *TransactionHandler) parseForm(sc projectScope, want transaction.Kind) (txInput, error) {
	if err := sc.req.ParseForm(); err != nil {
		return txInput{}, errBadForm
	}
	f := sc.req.PostForm
	kind := want
	if kind == "" {
		parsed, err := parseUserKind(f.Get("kind"))
		if err != nil {
			return txInput{}, err
		}
		kind = parsed
	}
	in := txInput{Kind: kind, Amount: strings.TrimSpace(f.Get("amount")), Note: strings.TrimSpace(f.Get("note"))}

	var err error
	in.WalletID, err = uuid.Parse(strings.TrimSpace(f.Get("wallet_id")))
	if err != nil {
		return txInput{}, errBadForm
	}
	if kind == transaction.KindTransfer {
		to, tErr := uuid.Parse(strings.TrimSpace(f.Get("to_wallet_id")))
		if tErr != nil {
			return txInput{}, errBadForm
		}
		in.ToWalletID = &to
	}
	if raw := strings.TrimSpace(f.Get("category_id")); raw != "" && kind.AllowsCategory() {
		id, cErr := uuid.Parse(raw)
		if cErr != nil {
			return txInput{}, errBadForm
		}
		in.CategoryID = &id
	}
	if raw := strings.TrimSpace(f.Get("period_id")); raw != "" {
		id, pErr := uuid.Parse(raw)
		if pErr != nil {
			return txInput{}, errBadForm
		}
		in.PeriodID = &id
	}
	in.Date = civil.DateOf(time.Now(), h.location(sc))
	if raw := strings.TrimSpace(f.Get("date")); raw != "" {
		parsed, dErr := civil.ParseDate(raw)
		if dErr != nil {
			return txInput{}, errBadForm
		}
		in.Date = parsed
	}
	return in, nil
}

func (h *TransactionHandler) amountFor(sc projectScope, in txInput) (money.Amount, error) {
	w, err := h.Wallets.ByID(sc.req.Context(), in.WalletID)
	if err != nil {
		return money.Amount{}, err
	}
	a, err := money.ParseMajor(in.Amount, w.Currency)
	if err != nil {
		return money.Amount{}, &transaction.InvalidAmountError{Reason: "not a number"}
	}
	return a, nil
}

// NOTE: local noon keeps a timezone shift from moving the date across a day boundary.
func (h *TransactionHandler) occurredAt(sc projectScope, in txInput) time.Time {
	return in.Date.In(h.location(sc)).Add(12 * time.Hour)
}

func (h *TransactionHandler) rememberWallet(w http.ResponseWriter, id uuid.UUID) {
	web.SetCookie(w, web.CookieOpts{
		Name:         LastWalletCookie,
		Value:        id.String(),
		BasePath:     h.Cfg.HTTP.BasePath,
		CookieSecure: h.Cfg.HTTP.CookieSecure,
		MaxAge:       lastWalletMaxAge,
	})
}

func (h *TransactionHandler) succeed(w http.ResponseWriter, sc projectScope, t *transaction.Transaction) {
	if !h.isHTMX(sc.req) {
		http.Redirect(w, sc.req, web.Path(h.Cfg.HTTP.BasePath,
			"/orgs/"+sc.org.Slug+"/projects/"+sc.project.Slug+"/overview"), http.StatusSeeOther)
		return
	}
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "transactions")
	list, err := h.listAfterWrite(sc, d)
	if err != nil {
		h.LogErr("web transaction: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load transactions.", err)
		return
	}
	msg := ""
	if t != nil {
		msg = d.Tr("tx.saved", "Amount", templates.Money(d, t.Amount))
	}
	Render(w, sc.req, templates.TransactionList(d, list))
	Render(w, sc.req, templates.Toast(d, msg))
}

func (h *TransactionHandler) refuse(w http.ResponseWriter, sc projectScope, cause error) {
	ae, ok := apperror.AsAppError(cause)
	if !ok {
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Save failed", "Could not record that transaction.", cause)
		return
	}
	d := h.LayoutForProject(sc.req, h.title(sc, "tx.title"), sc.org.Slug, sc.project, "transactions")
	list, err := h.listAfterWrite(sc, d)
	if err != nil {
		h.LogErr("web transaction: list", err)
		list = templates.TxListView{ProjectSlug: sc.project.Slug}
	}
	list.Error = ae.Message()
	if transaction.IsPeriodLockedError(cause) {
		list.Error = d.Tr("tx.locked")
	}
	list.ErrorCode = ae.Code()
	if h.isHTMX(sc.req) {
		Render(w, sc.req, templates.TransactionList(d, list))
		return
	}
	h.refusePage(w, sc, d, list)
}

func (h *TransactionHandler) refusePage(w http.ResponseWriter, sc projectScope, d web.LayoutData, list templates.TxListView) {
	form, err := h.formView(sc, d, txInput{Kind: transaction.KindExpense}, "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Save failed", "Could not record that transaction.", err)
		return
	}
	v := templates.TransactionsView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Form:        form,
		List:        list,
	}
	if err := h.fillFilters(sc, d, &v, h.activeFilters(sc)); err != nil {
		h.LogErr("web transaction: filters", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Save failed", "Could not record that transaction.", err)
		return
	}
	Render(w, sc.req, templates.TransactionsLayout(d, v))
}

func (h *TransactionHandler) requireTransaction(w http.ResponseWriter, r *http.Request) (projectScope, *transaction.Transaction, bool) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed transaction id.")
		return projectScope{}, nil, false
	}
	t, err := h.Transactions.ByID(sc.req.Context(), id)
	if err != nil {
		if transaction.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That transaction no longer exists.")
			return projectScope{}, nil, false
		}
		h.LogErr("web transaction: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that transaction.", err)
		return projectScope{}, nil, false
	}
	return sc, t, true
}

func (h *TransactionHandler) listView(sc projectScope, d web.LayoutData, q url.Values, limit int) (templates.TxListView, error) {
	opts := transaction.ListOpts{Limit: limit, Search: strings.TrimSpace(q.Get("q"))}
	if id, err := uuid.Parse(q.Get("wallet")); err == nil {
		opts.WalletID = &id
	}
	if id, err := uuid.Parse(q.Get("category")); err == nil {
		opts.CategoryID = &id
	}
	if id, err := uuid.Parse(q.Get("period")); err == nil {
		opts.PeriodID = &id
	}
	if k, err := transaction.ParseKind(q.Get("kind")); err == nil {
		opts.Kinds = []transaction.Kind{k}
	}
	if c, err := transaction.ParseCursor(q.Get("after")); err == nil {
		opts.After = &c
	}

	items, next, err := h.Transactions.List(sc.req.Context(), opts)
	if err != nil {
		return templates.TxListView{}, err
	}
	rows, err := h.rows(sc, d, items)
	if err != nil {
		return templates.TxListView{}, err
	}
	v := templates.TxListView{ProjectSlug: sc.project.Slug, Rows: rows}
	if next != nil {
		nq := url.Values{}
		for _, k := range txFilterKeys {
			if val := q.Get(k); val != "" {
				nq.Set(k, val)
			}
		}
		nq.Set("after", next.Encode())
		v.NextQuery = "?" + nq.Encode()
	}
	return v, nil
}

func (h *TransactionHandler) listAfterWrite(sc projectScope, d web.LayoutData) (templates.TxListView, error) {
	if sc.req.PostForm.Get("list") == "recent" {
		return h.recentView(sc, d)
	}
	return h.listView(sc, d, h.activeFilters(sc), txPageSize)
}

func (h *TransactionHandler) activeFilters(sc projectScope) url.Values {
	q := sc.req.URL.Query()
	if len(q) == 0 {
		if u, err := url.Parse(sc.req.Header.Get("HX-Current-URL")); err == nil {
			q = u.Query()
		}
	}
	out := url.Values{}
	for _, k := range txFilterKeys {
		if val := q.Get(k); val != "" {
			out.Set(k, val)
		}
	}
	return out
}

func (h *TransactionHandler) recentView(sc projectScope, d web.LayoutData) (templates.TxListView, error) {
	v, err := h.listView(sc, d, url.Values{}, txRecentLimit)
	if err != nil {
		return templates.TxListView{}, err
	}
	v.NextQuery = ""
	v.Compact = true
	return v, nil
}

func (h *TransactionHandler) rows(sc projectScope, d web.LayoutData, items []*transaction.Transaction) ([]templates.TxRow, error) {
	if len(items) == 0 {
		return nil, nil
	}
	wallets, err := h.walletNames(sc)
	if err != nil {
		return nil, err
	}
	cats, err := h.categoriesByID(sc)
	if err != nil {
		return nil, err
	}
	loc := h.location(sc)
	out := make([]templates.TxRow, 0, len(items))
	for _, t := range items {
		row := templates.TxRow{
			ID:         t.ID.String(),
			Kind:       string(t.Kind),
			KindLabel:  d.Tr(transactionKindKey(t.Kind)),
			Amount:     t.Amount,
			Inflow:     t.Kind.IsInflow(),
			Note:       t.Note,
			WalletName: wallets[t.WalletID],
			DateLabel:  t.OccurredAt.In(loc).Format("02 Jan"),
		}
		if t.ToWalletID != nil {
			row.ToWalletName = wallets[*t.ToWalletID]
		}
		if t.CategoryID != nil {
			if c, ok := cats[*t.CategoryID]; ok {
				row.CategoryName, row.CategoryIcon = c.Name, c.Icon
			}
		} else if t.Kind.AllowsCategory() {
			row.CategoryName = d.Tr("tx.uncategorized")
		}
		out = append(out, row)
	}
	return out, nil
}

func (h *TransactionHandler) formView(sc projectScope, d web.LayoutData, in txInput, id string) (templates.TxFormView, error) {
	loc := h.location(sc)
	today := civil.DateOf(time.Now(), loc)
	if in.Date.IsZero() {
		in.Date = today
	}
	v := templates.TxFormView{
		ProjectSlug: sc.project.Slug,
		ID:          id,
		Kind:        string(in.Kind),
		Amount:      in.Amount,
		Date:        in.Date.String(),
		Yesterday:   today.AddDays(-1).String(),
		Note:        in.Note,
	}
	if in.ToWalletID != nil {
		v.ToWalletID = in.ToWalletID.String()
	}
	if in.CategoryID != nil {
		v.CategoryID = in.CategoryID.String()
	}
	if in.PeriodID != nil {
		v.PeriodID = in.PeriodID.String()
	}

	wallets, err := h.Wallets.List(sc.req.Context(), wallet.ListOpts{})
	if err != nil {
		return templates.TxFormView{}, err
	}
	balances, err := h.Transactions.Balances(sc.req.Context())
	if err != nil {
		return templates.TxFormView{}, err
	}
	want := h.preferredWallet(sc, in, wallets)
	cur := h.currency(sc)
	for _, wl := range wallets {
		balance, ok := balances[wl.ID]
		if !ok {
			balance = money.Zero(wl.Currency)
		}
		selected := wl.ID == want
		if selected {
			cur = wl.Currency
		}
		v.Wallets = append(v.Wallets, templates.TxWalletOption{
			ID:       wl.ID.String(),
			Name:     wl.Name,
			Balance:  balance,
			Selected: selected,
			Symbol:   txCurrencySymbol(d, wl.Currency),
			Group:    wl.Currency.DisplayExponent() == 0,
		})
	}
	if want != uuid.Nil {
		v.WalletID = want.String()
	}
	v.Symbol = txCurrencySymbol(d, cur)
	v.Group = cur.DisplayExponent() == 0

	cats, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{})
	if err != nil {
		return templates.TxFormView{}, err
	}
	for _, c := range cats {
		v.Categories = append(v.Categories, templates.TxCategoryOption{
			ID:        c.ID.String(),
			Name:      c.Name,
			Icon:      c.Icon,
			Kind:      string(c.Kind),
			Selected:  in.CategoryID != nil && *in.CategoryID == c.ID,
			Suggested: in.Suggested != nil && *in.Suggested == c.ID,
		})
	}
	v.Periods = h.periodOptions(sc, in)
	return v, nil
}

func (h *TransactionHandler) preferredWallet(sc projectScope, in txInput, wallets []*wallet.Wallet) uuid.UUID {
	if in.WalletID != uuid.Nil {
		return in.WalletID
	}
	if c, err := sc.req.Cookie(LastWalletCookie); err == nil {
		if id, pErr := uuid.Parse(c.Value); pErr == nil {
			for _, wl := range wallets {
				if wl.ID == id {
					return id
				}
			}
		}
	}
	if len(wallets) > 0 {
		return wallets[0].ID
	}
	return uuid.Nil
}

func (h *TransactionHandler) periodOptions(sc projectScope, in txInput) []templates.TxPeriodOption {
	cur, err := h.Periods.Current(sc.req.Context())
	if err != nil {
		if !period.IsNotFoundError(err) {
			h.LogErr("web transaction: current period", err)
		}
		return nil
	}
	prev, next, err := h.Periods.Neighbors(sc.req.Context(), cur.ID)
	if err != nil {
		h.LogErr("web transaction: period neighbors", err)
	}
	out := make([]templates.TxPeriodOption, 0, 3)
	for _, p := range []*period.Period{prev, cur, next} {
		if p == nil {
			continue
		}
		out = append(out, templates.TxPeriodOption{
			ID:       p.ID.String(),
			Label:    p.Name,
			Selected: in.PeriodID != nil && *in.PeriodID == p.ID,
		})
	}
	return out
}

func (h *TransactionHandler) fillFilters(sc projectScope, d web.LayoutData, v *templates.TransactionsView, q url.Values) error {
	wallets, err := h.Wallets.List(sc.req.Context(), wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		return err
	}
	for _, wl := range wallets {
		v.Wallets = append(v.Wallets, templates.TxFilter{
			Value: wl.ID.String(), Label: wl.Name, Selected: q.Get("wallet") == wl.ID.String(),
		})
	}
	cats, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{IncludeArchived: true})
	if err != nil {
		return err
	}
	for _, c := range cats {
		v.Categories = append(v.Categories, templates.TxFilter{
			Value: c.ID.String(), Label: c.Name, Selected: q.Get("category") == c.ID.String(),
		})
	}
	periods, err := h.Periods.List(sc.req.Context(), period.ListOpts{Limit: txPeriodFilters})
	if err != nil {
		return err
	}
	for _, p := range periods {
		v.Periods = append(v.Periods, templates.TxFilter{
			Value: p.ID.String(), Label: p.Name, Selected: q.Get("period") == p.ID.String(),
		})
	}
	for _, k := range txUserKinds {
		v.Kinds = append(v.Kinds, templates.TxFilter{
			Value: string(k), Label: d.Tr(transactionKindKey(k)), Selected: q.Get("kind") == string(k),
		})
	}
	return nil
}

func (h *TransactionHandler) walletNames(sc projectScope) (map[uuid.UUID]string, error) {
	items, err := h.Wallets.List(sc.req.Context(), wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(items))
	for _, w := range items {
		out[w.ID] = w.Name
	}
	return out, nil
}

func (h *TransactionHandler) categoriesByID(sc projectScope) (map[uuid.UUID]*category.Category, error) {
	items, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*category.Category, len(items))
	for _, c := range items {
		out[c.ID] = c
	}
	return out, nil
}

func (h *TransactionHandler) location(sc projectScope) *time.Location {
	s, err := h.Ledgers.Get(sc.req.Context())
	if err != nil || s == nil {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (h *TransactionHandler) currency(sc projectScope) money.Currency {
	s, err := h.Ledgers.Get(sc.req.Context())
	if err != nil || s == nil {
		return money.IDR
	}
	return s.Currency
}

func (h *TransactionHandler) title(sc projectScope, key string) string {
	return h.Base(sc.req, "").Tr(key) + " · " + sc.project.Name
}

func (h *TransactionHandler) isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") != "" }

func txCurrencySymbol(d web.LayoutData, c money.Currency) string {
	return strings.TrimRight(templates.Money(d, money.Zero(c)), "0123456789.,\u00a0 ")
}

func parseUserKind(s string) (transaction.Kind, error) {
	k, err := transaction.ParseKind(strings.TrimSpace(s))
	if err != nil || !slices.Contains(txUserKinds, k) {
		return "", errBadForm
	}
	return k, nil
}

func transactionKindKey(k transaction.Kind) string {
	switch k {
	case transaction.KindIncome:
		return "tx.kind_income"
	case transaction.KindTransfer:
		return "tx.kind_transfer"
	case transaction.KindOpening:
		return "tx.kind_opening"
	case transaction.KindAdjustmentIn, transaction.KindAdjustmentOut:
		return "tx.kind_adjustment"
	case transaction.KindExpense:
		return "tx.kind_expense"
	}
	return "tx.kind_expense"
}
