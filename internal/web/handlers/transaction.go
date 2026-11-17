package handlers

import (
	"cmp"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
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

// ToastCookie carries the toast for the page a sheet save or a delete returns to: the saved entry, or the deleted one with its Undo.
const ToastCookie = "yasaku_toast"

const (
	lastWalletMaxAge = 365 * 24 * 60 * 60
	toastMaxAge      = 60
	toastCookieMax   = 3500
	txPageSize       = 50
	txRecentLimit    = 10
	txPeriodFilters  = 12
	txTopWindow      = 90 * 24 * time.Hour
	txTopLimit       = 50
	txRecentCats     = 8
	txRecentPerCat   = 3
	toastSaved       = "saved"
	toastDeleted     = "deleted"
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
func (h *TransactionHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions", h.GetList)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions/new", h.GetNew)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions", h.PostCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/suggest", h.PostSuggest)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions/{id}", h.GetDetail)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/transactions/{id}/edit", h.GetEdit)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/{id}", h.PostUpdate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/transactions/{id}/delete", h.PostDelete)
}

// GetList renders the transactions page, or just the list for an HTMX filter or load-more request.
func (h *TransactionHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "nav.transactions", sc.project.Name), sc.org.Slug, sc.project, "transactions")
	q := sc.req.URL.Query()

	list, err := h.listView(sc, d, q, txPageSize)
	if err != nil {
		h.LogErr("web transaction: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load transactions.", err)
		return
	}
	if q.Get("after") != "" {
		Render(w, sc.req, templates.TxRowsPage(d, list))
		return
	}
	if web.IsHTMXRequest(sc.req) {
		Render(w, sc.req, templates.TransactionList(d, list))
		return
	}

	form, err := h.formView(sc, d, txInput{Kind: transaction.KindExpense}, "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load transactions.", err)
		return
	}
	v := templates.TransactionsView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Form:        form,
		List:        list,
		Search:      q.Get("q"),
		Toast:       h.takeTxToast(w, sc, d),
	}
	if err := h.fillFilters(sc, d, &v, q); err != nil {
		h.LogErr("web transaction: filters", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load transactions.", err)
		return
	}
	Render(w, sc.req, templates.TransactionsLayout(d, v))
}

// GetNew renders the standalone quick-add page, amount first.
func (h *TransactionHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "title.tx_new", sc.project.Name), sc.org.Slug, sc.project, "transactions")
	form, err := h.formView(sc, d, h.newInput(sc), "")
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load the form.", err)
		return
	}
	if web.IsHTMXRequest(sc.req) {
		Render(w, sc.req, templates.TxSheet(d, form))
		return
	}
	Render(w, sc.req, templates.TransactionFormLayout(d, form, d.Tr("tx.new")))
}

// GetDetail renders one entry with its Edit, Duplicate and Delete actions.
func (h *TransactionHandler) GetDetail(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "title.tx_detail", sc.project.Name), sc.org.Slug, sc.project, "transactions")
	rows, err := h.rows(sc, d, []*transaction.Transaction{t})
	if err != nil {
		h.LogErr("web transaction: detail", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load that transaction.", err)
		return
	}
	v := templates.TxDetailView{
		ProjectSlug: sc.project.Slug,
		Row:         rows[0],
		RecordedAt:  t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		Locked:      h.periodLocked(sc, t.PeriodID),
		PastDay:     rows[0].Day != civil.DateOf(time.Now(), h.location(sc)),
	}
	if web.IsHTMXRequest(sc.req) {
		if u, err := url.Parse(sc.req.Header.Get("HX-Current-URL")); err == nil {
			v.Return = templates.TxReturnFor(u.Path)
		}
		Render(w, sc.req, templates.TxDetailDrawer(d, v))
		return
	}
	Render(w, sc.req, templates.TransactionDetailLayout(d, v))
}

// NOTE: the add-sheet URL contract: kind, category and wallet preselect; from duplicates an entry to today.
func (h *TransactionHandler) newInput(sc ProjectScope) txInput {
	q := sc.req.URL.Query()
	in := txInput{Kind: transaction.KindExpense}
	if id, err := uuid.Parse(q.Get("from")); err == nil {
		if t, tErr := h.Transactions.ByID(sc.req.Context(), id); tErr == nil && t.Kind.IsUserRecorded() {
			return txInput{
				Kind:       t.Kind,
				Amount:     amountInput(t.Amount, i18n.From(sc.req.Context()).Tag()),
				WalletID:   t.WalletID,
				ToWalletID: t.ToWalletID,
				CategoryID: t.CategoryID,
				Note:       t.Note,
			}
		}
	}
	if k, err := parseUserKind(q.Get("kind")); err == nil {
		in.Kind = k
	}
	if id, err := uuid.Parse(q.Get("category")); err == nil {
		in.CategoryID = &id
	}
	if id, err := uuid.Parse(q.Get("wallet")); err == nil {
		in.WalletID = id
	}
	return in
}

func (h *TransactionHandler) periodLocked(sc ProjectScope, id *uuid.UUID) bool {
	if id == nil {
		return false
	}
	p, err := h.Periods.ByID(sc.req.Context(), *id)
	if err != nil {
		return false
	}
	return p.IsLocked()
}

// GetEdit renders the edit form for one transaction.
func (h *TransactionHandler) GetEdit(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "title.tx_edit", sc.project.Name), sc.org.Slug, sc.project, "transactions")
	in := txInput{
		Kind:       t.Kind,
		Amount:     amountInput(t.Amount, i18n.From(sc.req.Context()).Tag()),
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
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load that transaction.", err)
		return
	}
	form.RecordedAt, form.UpdatedAt = t.CreatedAt, t.UpdatedAt
	Render(w, sc.req, templates.TransactionFormLayout(d, form, d.Tr("tx.edit")))
}

// PostCreate records one transaction and returns the refreshed list plus an out-of-band toast.
func (h *TransactionHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	in, amount, ok := h.readForm(w, sc, "", "")
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
		h.refuse(w, sc, err, txDraft{in: in, filled: true})
		return
	}
	h.rememberWallet(w, sc.req, t.WalletID)
	h.succeed(w, sc, t)
}

// PostUpdate revises one transaction and returns the refreshed list plus an out-of-band toast.
func (h *TransactionHandler) PostUpdate(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTransaction(w, r)
	if !ok {
		return
	}
	in, amount, ok := h.readForm(w, sc, t.Kind, t.ID.String())
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
		h.refuse(w, sc, err, txDraft{in: in, id: t.ID.String(), filled: true})
		return
	}
	h.rememberWallet(w, sc.req, updated.WalletID)
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
		h.refuse(w, sc, err, txDraft{})
		return
	}
	h.setTxToast(w, sc, toastDeleted, t)
	h.redirect(w, sc, h.returnPath(sc, sc.req.PostFormValue("return"), "transactions"))
}

// PostSuggest returns the category chips with the category last used for this note preselected.
func (h *TransactionHandler) PostSuggest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
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
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load categories.", err)
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

func txFormTitleKey(id string) string {
	if id == "" {
		return "tx.new"
	}
	return "tx.edit"
}

type txDraft struct {
	in     txInput
	id     string
	filled bool
}

func (h *TransactionHandler) readForm(w http.ResponseWriter, sc ProjectScope, want transaction.Kind, id string) (txInput, money.Amount, bool) {
	in, err := h.parseForm(sc, want)
	if err != nil {
		if errors.Is(err, errBadForm) {
			h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not read that form.")
			return txInput{}, money.Amount{}, false
		}
		h.refuse(w, sc, err, txDraft{in: in, id: id, filled: in.Kind != ""})
		return txInput{}, money.Amount{}, false
	}
	amount, err := h.amountFor(sc, in)
	if err != nil {
		h.refuse(w, sc, err, txDraft{in: in, id: id, filled: true})
		return txInput{}, money.Amount{}, false
	}
	return in, amount, true
}

// SECURITY: want pins the kind the stored row already has, so a posted kind can never switch it.
func (h *TransactionHandler) parseForm(sc ProjectScope, want transaction.Kind) (txInput, error) {
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

	rawWallet := strings.TrimSpace(f.Get("wallet_id"))
	if rawWallet == "" {
		return in, &wallet.NotFoundError{}
	}
	var err error
	in.WalletID, err = uuid.Parse(rawWallet)
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

func (h *TransactionHandler) amountFor(sc ProjectScope, in txInput) (money.Amount, error) {
	w, err := h.Wallets.ByID(sc.req.Context(), in.WalletID)
	if err != nil {
		return money.Amount{}, err
	}
	return parseAmount(in.Amount, w.Currency, i18n.From(sc.req.Context()).Tag())
}

// NOTE: local noon keeps a timezone shift from moving the date across a day boundary.
func (h *TransactionHandler) occurredAt(sc ProjectScope, in txInput) time.Time {
	return in.Date.In(h.location(sc)).Add(12 * time.Hour)
}

func (h *TransactionHandler) rememberWallet(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	web.SetCookie(w, web.CookieOpts{
		Name:         LastWalletCookie,
		Value:        id.String(),
		BasePath:     h.Cfg.HTTP.BasePath,
		CookieSecure: h.SecureCookie(r),
		MaxAge:       lastWalletMaxAge,
	})
}

func (h *TransactionHandler) succeed(w http.ResponseWriter, sc ProjectScope, t *transaction.Transaction) {
	if web.IsHTMXRequest(sc.req) && sc.req.PostForm.Get("again") == "1" {
		d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "transactions")
		Render(w, sc.req, templates.Toast(d, d.Tr("tx.saved", "Amount", templates.Money(d, t.Amount))))
		return
	}
	h.setTxToast(w, sc, toastSaved, t)
	h.redirect(w, sc, h.returnPath(sc, sc.req.PostForm.Get("return"), "overview"))
}

// NOTE: an htmx request gets HX-Redirect, so the sheet never swaps a region the opener page may not have.
func (h *TransactionHandler) redirect(w http.ResponseWriter, sc ProjectScope, to string) {
	if web.IsHTMXRequest(sc.req) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, sc.req, to, http.StatusSeeOther) //nolint:gosec // G710: to comes from returnPath, built from stored slugs and an allowlisted page
}

// SECURITY: templates.TxReturnPage is the allowlist, so a posted return can never leave the project.
func (h *TransactionHandler) returnPath(sc ProjectScope, raw, fallback string) string {
	page := cmp.Or(templates.TxReturnPage(raw), fallback)
	return web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/"+page))
}

func (h *TransactionHandler) refuse(w http.ResponseWriter, sc ProjectScope, cause error, draft txDraft) {
	ae, ok := apperror.AsAppError(cause)
	if !ok {
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not record that transaction.", cause)
		return
	}
	d := h.LayoutForProject(sc.req, pageTitle(sc.req, "nav.transactions", sc.project.Name), sc.org.Slug, sc.project, "transactions")
	list, err := h.listAfterWrite(sc, d)
	if err != nil {
		h.LogErr("web transaction: list", err)
		list = templates.TxListView{ProjectSlug: sc.project.Slug}
	}
	list.Error = ae.Message()
	if transaction.IsPeriodLockedError(cause) {
		list.Error = d.Tr("tx.locked")
	}
	amountErr, walletErr := fieldErrors(d, cause)
	if amountErr != "" || walletErr != "" {
		draft.filled = true
	}
	list.ErrorCode = ae.Code()
	if web.IsHTMXRequest(sc.req) && draft.filled {
		form, err := h.formView(sc, d, draft.in, draft.id)
		if err != nil {
			h.LogErr("web transaction: form", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not record that transaction.", err)
			return
		}
		form.InDialog = sc.req.PostForm.Get("surface") == "dialog"
		form.Return = sc.req.PostForm.Get("return")
		form.Recent = sc.req.PostForm.Get("list") == "recent"
		form.AmountError, form.WalletError = amountErr, walletErr
		if amountErr == "" && walletErr == "" {
			form.Error, form.ErrorCode = list.Error, list.ErrorCode
		}
		RenderStatus(w, sc.req, http.StatusUnprocessableEntity, templates.TransactionForm(d, form))
		return
	}
	if web.IsHTMXRequest(sc.req) {
		RenderStatus(w, sc.req, http.StatusUnprocessableEntity, templates.TransactionList(d, list))
		return
	}
	h.refusePage(w, sc, d, list, draft, amountErr, walletErr)
}

//i18n:use tx.wallet_error.required
func fieldErrors(d web.LayoutData, cause error) (amount, walletErr string) {
	if key := amountErrorKey(cause); key != "" {
		return d.Tr(key), ""
	}
	if wallet.IsNotFoundError(cause) {
		return "", d.Tr("tx.wallet_error.required")
	}
	return "", ""
}

func (h *TransactionHandler) refusePage(w http.ResponseWriter, sc ProjectScope, d web.LayoutData, list templates.TxListView, draft txDraft, amountErr, walletErr string) {
	in := draft.in
	if !draft.filled {
		in = txInput{Kind: transaction.KindExpense}
	}
	form, err := h.formView(sc, d, in, draft.id)
	if err != nil {
		h.LogErr("web transaction: form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not record that transaction.", err)
		return
	}
	if draft.filled || draft.id != "" {
		form.AmountError, form.WalletError = amountErr, walletErr
		if amountErr == "" && walletErr == "" {
			form.Error, form.ErrorCode = list.Error, list.ErrorCode
		}
		Render(w, sc.req, templates.TransactionFormLayout(d, form, d.Tr(txFormTitleKey(draft.id))))
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
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not record that transaction.", err)
		return
	}
	Render(w, sc.req, templates.TransactionsLayout(d, v))
}

func (h *TransactionHandler) requireTransaction(w http.ResponseWriter, r *http.Request) (ProjectScope, *transaction.Transaction, bool) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return ProjectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_id"), "Malformed transaction id.")
		return ProjectScope{}, nil, false
	}
	t, err := h.Transactions.ByID(sc.req.Context(), id)
	if err != nil {
		if transaction.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_found"), "That transaction no longer exists.")
			return ProjectScope{}, nil, false
		}
		h.LogErr("web transaction: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.lookup_failed"), "Could not load that transaction.", err)
		return ProjectScope{}, nil, false
	}
	// SECURITY: opening and adjustment rows are system-written; editing one rewrites a derived wallet balance.
	if !t.Kind.IsUserRecorded() {
		h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_editable"),
			"That entry was recorded by yasaku, so it cannot be edited here.")
		return ProjectScope{}, nil, false
	}
	return sc, t, true
}

func (h *TransactionHandler) listView(sc ProjectScope, d web.LayoutData, q url.Values, limit int) (templates.TxListView, error) {
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

func (h *TransactionHandler) listAfterWrite(sc ProjectScope, d web.LayoutData) (templates.TxListView, error) {
	if sc.req.PostForm.Get("list") == "recent" {
		return h.recentView(sc, d)
	}
	return h.listView(sc, d, h.activeFilters(sc), txPageSize)
}

func (h *TransactionHandler) activeFilters(sc ProjectScope) url.Values {
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

func (h *TransactionHandler) recentView(sc ProjectScope, d web.LayoutData) (templates.TxListView, error) {
	v, err := h.listView(sc, d, url.Values{}, txRecentLimit)
	if err != nil {
		return templates.TxListView{}, err
	}
	v.NextQuery = ""
	v.Compact = true
	return v, nil
}

func (h *TransactionHandler) rows(sc ProjectScope, d web.LayoutData, items []*transaction.Transaction) ([]templates.TxRow, error) {
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
			Day:        civil.DateOf(t.OccurredAt, loc),
			OccurredAt: t.OccurredAt,
			Editable:   t.Kind.IsUserRecorded(),
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

func (h *TransactionHandler) formView(sc ProjectScope, d web.LayoutData, in txInput, id string) (templates.TxFormView, error) {
	loc := h.location(sc)
	today := civil.DateOf(time.Now(), loc)
	if in.Date.IsZero() {
		in.Date = today
	}
	tag := i18n.From(sc.req.Context()).Tag()
	v := templates.TxFormView{
		ProjectSlug: sc.project.Slug,
		ID:          id,
		Kind:        string(in.Kind),
		Amount:      in.Amount,
		Date:        in.Date.String(),
		Today:       today.String(),
		Yesterday:   today.AddDays(-1).String(),
		Note:        in.Note,
		Decimal:     string(money.DecimalMark(tag)),
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
			Symbol:   templates.CurrencySymbol(d, wl.Currency),
			Currency: string(wl.Currency),
			Exponent: wl.Currency.DisplayExponent(),
		})
	}
	if want != uuid.Nil {
		v.WalletID = want.String()
	}
	v.NoWallet = len(wallets) == 0
	v.Symbol = templates.CurrencySymbol(d, cur)
	v.Currency = string(cur)
	v.Exponent = cur.DisplayExponent()

	cats, err := h.TxCategories.List(sc.req.Context(), category.ListOpts{})
	if err != nil {
		return templates.TxFormView{}, err
	}
	cats = h.byFrequency(sc, cats)
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
	if id == "" {
		v.RecentEntries = h.recentOptions(sc, cats, tag)
	}
	return v, nil
}

func (h *TransactionHandler) byFrequency(sc ProjectScope, cats []*category.Category) []*category.Category {
	top, err := h.Transactions.TopCategories(sc.req.Context(), time.Now().Add(-txTopWindow), txTopLimit)
	if err != nil {
		h.LogErr("web transaction: top categories", err)
		return cats
	}
	rank := make(map[uuid.UUID]int, len(top))
	for i, c := range top {
		rank[c.CategoryID] = len(top) - i
	}
	out := slices.Clone(cats)
	slices.SortStableFunc(out, func(a, b *category.Category) int { return rank[b.ID] - rank[a.ID] })
	return out
}

func (h *TransactionHandler) recentOptions(sc ProjectScope, cats []*category.Category, tag language.Tag) []templates.TxRecentOption {
	var out []templates.TxRecentOption
	for i, c := range cats {
		if i == txRecentCats {
			break
		}
		items, err := h.Transactions.RecentByCategory(sc.req.Context(), c.ID, txRecentPerCat)
		if err != nil {
			h.LogErr("web transaction: recent", err)
			return out
		}
		for _, e := range items {
			out = append(out, templates.TxRecentOption{
				CategoryID: c.ID.String(),
				Kind:       string(c.Kind),
				Icon:       c.Icon,
				Note:       e.Note,
				Label:      cmp.Or(e.Note, c.Name),
				Amount:     e.Amount,
				Input:      amountInput(e.Amount, tag),
				WalletID:   e.WalletID.String(),
			})
		}
	}
	return out
}

func (h *TransactionHandler) preferredWallet(sc ProjectScope, in txInput, wallets []*wallet.Wallet) uuid.UUID {
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
	if len(wallets) == 0 {
		return uuid.Nil
	}
	return slices.MinFunc(wallets, func(a, b *wallet.Wallet) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID.String(), b.ID.String()))
	}).ID
}

func (h *TransactionHandler) periodOptions(sc ProjectScope, in txInput) []templates.TxPeriodOption {
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

func (h *TransactionHandler) fillFilters(sc ProjectScope, d web.LayoutData, v *templates.TransactionsView, q url.Values) error {
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

func (h *TransactionHandler) walletNames(sc ProjectScope) (map[uuid.UUID]string, error) {
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

func (h *TransactionHandler) categoriesByID(sc ProjectScope) (map[uuid.UUID]*category.Category, error) {
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

func (h *TransactionHandler) location(sc ProjectScope) *time.Location {
	if loc := h.ProjectZone(sc); loc != nil {
		return loc
	}
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

func (h *TransactionHandler) currency(sc ProjectScope) money.Currency {
	s, err := h.Ledgers.Get(sc.req.Context())
	if err != nil || s == nil {
		return money.IDR
	}
	return s.Currency
}

// NOTE: the amount field's value in the locale's decimal mark, ungrouped, so ParseLocale reads it back unchanged.
func amountInput(a money.Amount, tag language.Tag) string {
	return strings.Replace(a.Major(), ".", string(money.DecimalMark(tag)), 1)
}

func toastValues(kind string, t *transaction.Transaction, loc *time.Location) url.Values {
	v := url.Values{
		"toast":     {kind},
		"kind":      {string(t.Kind)},
		"minor":     {strconv.FormatInt(t.Amount.Minor, 10)},
		"currency":  {string(t.Amount.Currency)},
		"wallet_id": {t.WalletID.String()},
		"date":      {civil.DateOf(t.OccurredAt, loc).String()},
	}
	if kind == toastDeleted {
		v.Set("note", t.Note)
	}
	if t.ToWalletID != nil {
		v.Set("to_wallet_id", t.ToWalletID.String())
	}
	if t.CategoryID != nil {
		v.Set("category_id", t.CategoryID.String())
	}
	return v
}

// NOTE: browsers drop a cookie over 4KB, so a note that would push it past toastCookieMax is left out.
func encodeToast(v url.Values) string {
	out := base64.RawURLEncoding.EncodeToString([]byte(v.Encode()))
	if len(out) <= toastCookieMax {
		return out
	}
	v.Del("note")
	return base64.RawURLEncoding.EncodeToString([]byte(v.Encode()))
}

func (h *TransactionHandler) setTxToast(w http.ResponseWriter, sc ProjectScope, kind string, t *transaction.Transaction) {
	web.SetCookie(w, web.CookieOpts{
		Name:         ToastCookie,
		Value:        encodeToast(toastValues(kind, t, h.location(sc))),
		BasePath:     projectCookiePath(h.Cfg.HTTP.BasePath, sc),
		CookieSecure: h.SecureCookie(sc.req),
		MaxAge:       toastMaxAge,
	})
}

func projectCookiePath(base string, sc ProjectScope) string {
	return web.Path(base, projectPath(sc.org.Slug, sc.project.Slug, ""))
}

// NOTE: any project page renders the toast a sheet save or delete left; an Undo only refills the normal create form, which validates every field again.
func (dp Deps) takeTxToast(w http.ResponseWriter, sc ProjectScope, d web.LayoutData) *templates.TxToastView {
	c, err := sc.req.Cookie(ToastCookie)
	if err != nil {
		return nil
	}
	web.ClearCookie(w, ToastCookie, projectCookiePath(dp.Cfg.HTTP.BasePath, sc), dp.SecureCookie(sc.req))
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	v, err := url.ParseQuery(string(raw))
	if err != nil {
		return nil
	}
	cur, err := money.ParseCurrency(v.Get("currency"))
	if err != nil {
		return nil
	}
	minor, err := strconv.ParseInt(v.Get("minor"), 10, 64)
	if err != nil {
		return nil
	}
	a := money.New(minor, cur)
	if v.Get("toast") == toastSaved {
		return &templates.TxToastView{Message: d.Tr("tx.saved", "Amount", templates.Money(d, a))}
	}
	return &templates.TxToastView{
		ProjectSlug: sc.project.Slug,
		Message:     d.Tr("tx.deleted", "Amount", templates.Money(d, a)),
		Undo:        true,
		Return:      templates.TxReturnFor(sc.req.URL.Path),
		Kind:        v.Get("kind"),
		Amount:      amountInput(a, i18n.From(sc.req.Context()).Tag()),
		WalletID:    v.Get("wallet_id"),
		ToWalletID:  v.Get("to_wallet_id"),
		CategoryID:  v.Get("category_id"),
		Date:        v.Get("date"),
		Note:        v.Get("note"),
	}
}

func parseUserKind(s string) (transaction.Kind, error) {
	k, err := transaction.ParseKind(strings.TrimSpace(s))
	if err != nil || !slices.Contains(txUserKinds, k) {
		return "", errBadForm
	}
	return k, nil
}

//i18n:use nav.transactions
//i18n:use tx.kind_expense
//i18n:use tx.kind_income
//i18n:use tx.kind_transfer
//i18n:use tx.kind_opening
//i18n:use tx.kind_adjustment
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
