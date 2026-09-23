package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	"altalune.id/yasaku/civil"
	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

// NOTE: a bare YYYY-MM-DD is pinned to local noon so the civil date survives a zone shift.
const noonHour = 12

func needs(field, reason string, candidates ...string) *yasakuv1.Needs {
	return &yasakuv1.Needs{
		Needs: []*yasakuv1.Need{{Field: field, Reason: reason, Candidates: candidates}},
	}
}

func validationError(field, reason string) *apperror.AppError {
	return apperror.New(
		apperror.CodeValidation,
		reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeValidation,
			Meta: map[string]string{"field": field, "reason": reason},
		},
	)
}

func invalidArg(field, reason string) error { return validationError(field, reason) }

func invalidArgCause(field, reason string, cause error) error {
	return validationError(field, reason).WithCause(cause)
}

// SECURITY: a missing row and a row in another tenant must be indistinguishable.
func scopeNotFound(field, value string) error {
	return apperror.New(
		apperror.CodeNotFound,
		strings.ToUpper(field[:1])+field[1:]+" not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeNotFound,
			Meta: map[string]string{"field": field, "value": value},
		},
	)
}

func parseMoney(m *yasakuv1.Money, fallback money.Currency) (money.Amount, error) {
	raw := strings.TrimSpace(m.GetAmount())
	if raw == "" {
		return money.Amount{}, invalidArg("amount", "amount is required")
	}
	cur := fallback
	if code := strings.TrimSpace(m.GetCurrency()); code != "" {
		parsed, err := money.ParseCurrency(code)
		if err != nil {
			return money.Amount{}, invalidArgCause("amount.currency", "unknown currency "+code, err)
		}
		cur = parsed
	}
	if cur == "" {
		cur = money.IDR
	}
	a, err := money.ParseMajor(raw, cur)
	if err != nil {
		return money.Amount{}, invalidArgCause("amount", "amount is not a valid number", err)
	}
	return a, nil
}

func toMoney(a money.Amount) *yasakuv1.Money {
	return &yasakuv1.Money{Amount: a.Major(), Currency: string(a.Currency)}
}

func parseWhen(field, raw string, loc *time.Location, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now, nil
	}
	if d, err := civil.ParseDate(raw); err == nil {
		return time.Date(d.Year, d.Month, d.Day, noonHour, 0, 0, 0, loc), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, invalidArgCause(field, "expected YYYY-MM-DD or an RFC 3339 instant", err)
	}
	return t, nil
}

func parseDate(field, s string) (civil.Date, error) {
	d, err := civil.ParseDate(strings.TrimSpace(s))
	if err != nil {
		return civil.Date{}, invalidArgCause(field, "expected YYYY-MM-DD", err)
	}
	return d, nil
}

func toTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func toProtoSettings(s *ledger.Settings) *yasakuv1.Settings {
	if s == nil {
		return nil
	}
	return &yasakuv1.Settings{
		Timezone:       s.Timezone,
		Currency:       string(s.Currency),
		PeriodStartDay: int32(s.PeriodStartDay), //nolint:gosec // bounded to 1..28 by the aggregate.
	}
}

func toProtoWallet(w *wallet.Wallet, balance *money.Amount) *yasakuv1.Wallet {
	if w == nil {
		return nil
	}
	out := &yasakuv1.Wallet{
		Id:               w.ID.String(),
		Name:             w.Name,
		Kind:             string(w.Kind),
		Provider:         w.Provider,
		Currency:         string(w.Currency),
		ExcludeFromTotal: w.ExcludeFromTotal,
		Archived:         w.IsArchived(),
		CreatedAt:        toTimestamp(w.CreatedAt),
	}
	if balance != nil {
		out.Balance = toMoney(*balance)
	}
	return out
}

func toWalletRef(w *wallet.Wallet) *yasakuv1.WalletRef {
	if w == nil {
		return nil
	}
	return &yasakuv1.WalletRef{Id: w.ID.String(), Name: w.Name}
}

func toProtoCategory(c *category.Category) *yasakuv1.Category {
	if c == nil {
		return nil
	}
	return &yasakuv1.Category{
		Id:       c.ID.String(),
		Name:     c.Name,
		Kind:     string(c.Kind),
		Icon:     c.Icon,
		Color:    c.Color,
		Archived: c.IsArchived(),
	}
}

func toProtoPeriod(p *period.Period) *yasakuv1.Period {
	if p == nil {
		return nil
	}
	out := &yasakuv1.Period{
		Id:        p.ID.String(),
		Name:      p.Name,
		StartDate: p.StartDate.String(),
		Status:    string(p.Status),
		Snapshot:  toProtoSnapshot(p.Snapshot),
	}
	if p.EndDate != nil {
		out.EndDate = p.EndDate.String()
	}
	if p.ClosedAt != nil {
		out.ClosedAt = toTimestamp(*p.ClosedAt)
	}
	return out
}

func toProtoSnapshot(s *period.Snapshot) *yasakuv1.Snapshot {
	if s == nil {
		return nil
	}
	out := &yasakuv1.Snapshot{
		Income:     toMoney(money.New(s.Income, s.Currency)),
		Expense:    toMoney(money.New(s.Expense, s.Currency)),
		Net:        toMoney(money.New(s.Net, s.Currency)),
		TxCount:    int32(s.TxCount), //nolint:gosec // a row count, far below int32.
		ComputedAt: toTimestamp(s.ComputedAt),
	}
	if len(s.Wallets) > 0 {
		out.Wallets = make([]*yasakuv1.WalletClosing, 0, len(s.Wallets))
		for _, w := range s.Wallets {
			out.Wallets = append(out.Wallets, &yasakuv1.WalletClosing{
				Wallet:  &yasakuv1.WalletRef{Id: w.WalletID.String(), Name: w.Name},
				Closing: toMoney(money.New(w.Closing, s.Currency)),
			})
		}
	}
	return out
}

func toProtoWalletLines(lines []report.WalletLine) []*yasakuv1.WalletLine {
	out := make([]*yasakuv1.WalletLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, &yasakuv1.WalletLine{
			Wallet:           &yasakuv1.WalletRef{Id: l.WalletID.String(), Name: l.Name},
			Kind:             l.Kind,
			ExcludeFromTotal: l.ExcludeFromTotal,
			Opening:          toMoney(l.Opening),
			In:               toMoney(l.In),
			Out:              toMoney(l.Out),
			Closing:          toMoney(l.Closing),
		})
	}
	return out
}

func toProtoCategorySlices(slices []report.CategorySlice) []*yasakuv1.CategorySlice {
	out := make([]*yasakuv1.CategorySlice, 0, len(slices))
	for _, s := range slices {
		ref := &yasakuv1.CategoryRef{Name: s.Name, Icon: s.Icon, Color: s.Color}
		if s.CategoryID != nil {
			ref.Id = s.CategoryID.String()
		}
		out = append(out, &yasakuv1.CategorySlice{
			Category: ref,
			Amount:   toMoney(s.Amount),
			Share:    s.Share,
			Count:    int32(s.Count), //nolint:gosec // a row count, far below int32.
		})
	}
	return out
}

type refCache struct {
	loc        *time.Location
	wallets    *wallet.Service
	categories *category.Service
	periods    *period.Service

	wallet   map[uuid.UUID]*wallet.Wallet
	category map[uuid.UUID]*category.Category
	period   map[uuid.UUID]*period.Period
}

func newRefCache(w *wallet.Service, c *category.Service, p *period.Service, loc *time.Location) *refCache {
	if loc == nil {
		loc = time.UTC
	}
	return &refCache{
		loc:        loc,
		wallets:    w,
		categories: c,
		periods:    p,
		wallet:     map[uuid.UUID]*wallet.Wallet{},
		category:   map[uuid.UUID]*category.Category{},
		period:     map[uuid.UUID]*period.Period{},
	}
}

// NOTE: a lookup failure degrades to a bare id rather than failing a read RPC over a display name.
func (r *refCache) walletRef(ctx context.Context, id uuid.UUID) *yasakuv1.WalletRef {
	if id == uuid.Nil {
		return nil
	}
	w, ok := r.wallet[id]
	if !ok {
		w, _ = r.wallets.ByID(ctx, id)
		r.wallet[id] = w
	}
	if w == nil {
		return &yasakuv1.WalletRef{Id: id.String()}
	}
	return toWalletRef(w)
}

func (r *refCache) categoryOf(ctx context.Context, id *uuid.UUID) *yasakuv1.Category {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	c, ok := r.category[*id]
	if !ok {
		c, _ = r.categories.ByID(ctx, *id)
		r.category[*id] = c
	}
	if c == nil {
		return &yasakuv1.Category{Id: id.String()}
	}
	return toProtoCategory(c)
}

func (r *refCache) periodName(ctx context.Context, id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	p, ok := r.period[*id]
	if !ok {
		p, _ = r.periods.ByID(ctx, *id)
		r.period[*id] = p
	}
	if p == nil {
		return ""
	}
	return p.Name
}

func (r *refCache) tx(ctx context.Context, t *transaction.Transaction) *yasakuv1.Transaction {
	if t == nil {
		return nil
	}
	out := &yasakuv1.Transaction{
		Id:         t.ID.String(),
		Kind:       string(t.Kind),
		Wallet:     r.walletRef(ctx, t.WalletID),
		Amount:     toMoney(t.Amount),
		Category:   r.categoryOf(ctx, t.CategoryID),
		Note:       t.Note,
		OccurredAt: toTimestamp(t.OccurredAt),
		CreatedAt:  toTimestamp(t.CreatedAt),
		Date:       civil.DateOf(t.OccurredAt, r.loc).String(),
	}
	if t.ToWalletID != nil {
		out.ToWallet = r.walletRef(ctx, *t.ToWalletID)
	}
	if t.PeriodID != nil {
		out.PeriodId = t.PeriodID.String()
		out.PeriodName = r.periodName(ctx, t.PeriodID)
	}
	return out
}

func (r *refCache) txs(ctx context.Context, rows []*transaction.Transaction) []*yasakuv1.Transaction {
	out := make([]*yasakuv1.Transaction, 0, len(rows))
	for _, t := range rows {
		out = append(out, r.tx(ctx, t))
	}
	return out
}

func pageTotals(rows []*transaction.Transaction) (totalIn, totalOut *yasakuv1.Money) {
	if len(rows) == 0 {
		return nil, nil
	}
	cur := rows[0].Amount.Currency
	in, out := money.Zero(cur), money.Zero(cur)
	for _, t := range rows {
		if t.Amount.Currency != cur {
			return nil, nil
		}
		if t.Kind.IsInflow() {
			in = in.Add(t.Amount)
			continue
		}
		out = out.Add(t.Amount)
	}
	return toMoney(in), toMoney(out)
}

// NOTE: no read response message carries a Needs field, so a read RPC must answer with an error.
func needsErr(nd *yasakuv1.Needs) error {
	if nd == nil || len(nd.GetNeeds()) == 0 {
		return invalidArg("target", "the request is ambiguous")
	}
	n := nd.GetNeeds()[0]
	msg := n.GetReason()
	if len(n.GetCandidates()) > 0 {
		msg += ": " + strings.Join(n.GetCandidates(), ", ")
	}
	return invalidArg(n.GetField(), msg)
}
