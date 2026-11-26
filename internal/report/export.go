package report

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

const utf8BOM = "\ufeff"

// ExportWallet is one wallet row of an export, archived or not.
type ExportWallet struct {
	ID               uuid.UUID
	Name             string
	Kind             string
	Provider         string
	Currency         money.Currency
	ExcludeFromTotal bool
	CreatedAt        time.Time
	ArchivedAt       *time.Time
}

// ExportCategory is one category row of an export, archived or not.
type ExportCategory struct {
	ID         uuid.UUID
	Name       string
	Kind       string
	CreatedAt  time.Time
	ArchivedAt *time.Time
}

// ExportTransaction is one transaction row of an export.
type ExportTransaction struct {
	ID         uuid.UUID
	OccurredAt time.Time
	Kind       string
	Amount     money.Amount
	WalletID   uuid.UUID
	ToWalletID *uuid.UUID
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Note       string
}

// ExportPeriod is one period row of an export.
type ExportPeriod struct {
	ID       uuid.UUID
	Name     string
	Start    civil.Date
	End      *civil.Date
	ClosedAt *time.Time
}

// ExportSource reads every row of the project on ctx that an export holds; boot backs it with the ledger services.
type ExportSource interface {
	Wallets(ctx context.Context) ([]ExportWallet, error)
	Categories(ctx context.Context) ([]ExportCategory, error)
	Transactions(ctx context.Context) ([]ExportTransaction, error)
	Periods(ctx context.Context) ([]ExportPeriod, error)
}

// Auditor records an export in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// Exporter writes a project's ledger as a zip of CSV files.
type Exporter struct {
	src        ExportSource
	settings   SettingsReader
	uow        tenant.UnitOfWork
	audit      Auditor
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewExporter binds the exporter to its dependencies.
func NewExporter(src ExportSource, settings SettingsReader, uow tenant.UnitOfWork, a Auditor, log *slog.Logger, unexpected apperror.UnexpectedFunc) *Exporter {
	return &Exporter{src: src, settings: settings, uow: uow, audit: a, log: log.With("module", "report"), unexpected: unexpected}
}

type exportData struct {
	wallets    []ExportWallet
	categories []ExportCategory
	txs        []ExportTransaction
	periods    []ExportPeriod
}

// WriteZip writes wallets.csv, categories.csv, transactions.csv and periods.csv to w, UTF-8 with a BOM, times in ISO 8601 in the project time zone, and records project.exported; nothing is written unless the caller holds all three reads outside a support visit.
func (e *Exporter) WriteZip(ctx context.Context, w io.Writer) error {
	ctx, span := tracer.Start(ctx, "report.Export")
	defer span.End()

	for _, p := range []authz.Permission{authz.WalletsRead, authz.CategoriesRead, authz.TransactionsRead} {
		if err := authz.Can(ctx, p); err != nil {
			return err
		}
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	// SECURITY: a read-only support visit sees the ledger but never takes it out.
	if tc.ReadOnly {
		return &authz.SupportReadOnlyError{}
	}
	loc, err := e.settings.Location(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return e.pass(ctx, "report.Export: location", err, tc)
	}
	if loc == nil {
		loc = time.UTC
	}
	d, err := e.read(ctx)
	if err != nil {
		return e.pass(ctx, "report.Export: read", err, tc)
	}
	err = e.uow(ctx, func(ctx context.Context) error {
		return e.audit.Record(ctx, audit.Event{Action: "project.exported", TargetKind: "project", TargetID: tc.ProjectID.String(),
			ProjectID: tc.ProjectID, Detail: map[string]string{
				"wallets": strconv.Itoa(len(d.wallets)), "categories": strconv.Itoa(len(d.categories)),
				"transactions": strconv.Itoa(len(d.txs)), "periods": strconv.Itoa(len(d.periods)),
			}})
	})
	if err != nil {
		return e.pass(ctx, "report.Export: audit", err, tc)
	}
	return writeExport(w, d, loc)
}

func (e *Exporter) read(ctx context.Context) (exportData, error) {
	var d exportData
	var err error
	if d.wallets, err = e.src.Wallets(ctx); err != nil {
		return d, err
	}
	if d.categories, err = e.src.Categories(ctx); err != nil {
		return d, err
	}
	if d.txs, err = e.src.Transactions(ctx); err != nil {
		return d, err
	}
	d.periods, err = e.src.Periods(ctx)
	return d, err
}

func (e *Exporter) pass(ctx context.Context, op string, err error, tc tenant.Context) error {
	if _, ok := apperror.AsAppError(err); ok {
		return err
	}
	return e.unexpected(ctx, op, err, "org_id", tc.OrgID, "project_id", tc.ProjectID)
}

func writeExport(w io.Writer, d exportData, loc *time.Location) error {
	ts := func(t time.Time) string { return t.In(loc).Format(time.RFC3339) }
	opt := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ts(*t)
	}
	id := func(u *uuid.UUID) string {
		if u == nil {
			return ""
		}
		return u.String()
	}
	files := []struct {
		name string
		rows [][]string
	}{
		{"wallets.csv", [][]string{{"id", "name", "kind", "provider", "currency", "exclude_from_total", "created_at", "archived_at"}}},
		{"categories.csv", [][]string{{"id", "name", "kind", "created_at", "archived_at"}}},
		{"transactions.csv", [][]string{{"id", "occurred_at", "kind", "amount", "currency", "wallet_id", "to_wallet_id", "category_id", "period_id", "note"}}},
		{"periods.csv", [][]string{{"id", "name", "start_date", "end_date", "closed_at"}}},
	}
	for _, x := range d.wallets {
		files[0].rows = append(files[0].rows, []string{x.ID.String(), cell(x.Name), x.Kind, cell(x.Provider), string(x.Currency),
			strconv.FormatBool(x.ExcludeFromTotal), ts(x.CreatedAt), opt(x.ArchivedAt)})
	}
	for _, x := range d.categories {
		files[1].rows = append(files[1].rows, []string{x.ID.String(), cell(x.Name), x.Kind, ts(x.CreatedAt), opt(x.ArchivedAt)})
	}
	for _, x := range d.txs {
		files[2].rows = append(files[2].rows, []string{x.ID.String(), ts(x.OccurredAt), x.Kind, x.Amount.Major(), string(x.Amount.Currency),
			x.WalletID.String(), id(x.ToWalletID), id(x.CategoryID), id(x.PeriodID), cell(x.Note)})
	}
	for _, x := range d.periods {
		end := ""
		if x.End != nil {
			end = x.End.String()
		}
		files[3].rows = append(files[3].rows, []string{x.ID.String(), cell(x.Name), x.Start.String(), end, opt(x.ClosedAt)})
	}
	zw := zip.NewWriter(w)
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return fmt.Errorf("report: export: %s: %w", f.name, err)
		}
		if _, err := io.WriteString(fw, utf8BOM); err != nil {
			return fmt.Errorf("report: export: %s: %w", f.name, err)
		}
		cw := csv.NewWriter(fw)
		if err := cw.WriteAll(f.rows); err != nil {
			return fmt.Errorf("report: export: %s: %w", f.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("report: export: close: %w", err)
	}
	return nil
}

// SECURITY: a typed text cell starting like a formula is quoted, so a spreadsheet opening the CSV never runs it; see OWASP CSV Injection.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
