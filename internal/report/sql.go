package report

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

const (
	kindExpense = "expense"
	kindIncome  = "income"
)

// maxCashflowPeriods bounds the generated IN list so every bind token stays the same width and cannot prefix another.
const maxCashflowPeriods = 9999

type tableNames struct {
	txn        string
	wallets    string
	categories string
	periods    string
}

func newTableNames(schema, prefix string) tableNames {
	qualify := func(name string) string {
		if schema == "" {
			return prefix + name
		}
		return schema + "." + prefix + name
	}
	return tableNames{
		txn:        qualify("transactions"),
		wallets:    qualify("wallets"),
		categories: qualify("categories"),
		periods:    qualify("periods"),
	}
}

// SECURITY: every read layers the caller's org, the request's tenant scope and the project, so a mismatched pair matches nothing.
const scopePredicate = `org_id = #orgID AND org_id = #scopeOrg AND project_id = #projectID`

const scopePredicateT = `t.org_id = #orgID AND t.org_id = #scopeOrg AND t.project_id = #projectID`

func summaryTotalsSQL(t tableNames, pg bool) string {
	return fmt.Sprintf(`
SELECT COALESCE(SUM(CASE WHEN kind IN ('income','adjustment_in')   THEN amount_minor ELSE 0 END), 0)%[2]s AS "totals.income",
       COALESCE(SUM(CASE WHEN kind IN ('expense','adjustment_out') THEN amount_minor ELSE 0 END), 0)%[2]s AS "totals.expense",
       COALESCE(SUM(CASE WHEN kind IN ('income','expense')         THEN 1 ELSE 0 END), 0)%[3]s AS "totals.tx_count"
  FROM %[1]s
 WHERE `+scopePredicate+` AND period_id = #periodID AND currency = #currency`,
		t.txn, bigintCast(pg), intCast(pg))
}

// priorPredicate matches rows keyed to an earlier period, plus unassigned rows before the period's local start.
const priorPredicate = `(
        t.period_id IN (
          SELECT p.id FROM %[2]s p
           WHERE p.org_id = #orgID AND p.project_id = #projectID
             AND p.start_date < (SELECT p2.start_date FROM %[2]s p2
                                  WHERE p2.id = #periodID AND p2.org_id = #orgID AND p2.project_id = #projectID)
        )
        OR (t.period_id IS NULL AND t.occurred_at < #startUTC)
      )`

func walletLinesSQL(t tableNames, pg bool) string {
	return fmt.Sprintf(`
SELECT w.id                 AS "line.wallet_id",
       w.name               AS "line.name",
       w.kind               AS "line.kind",
       w.exclude_from_total AS "line.exclude",
       COALESCE(SUM(m.opening), 0)%[4]s AS "line.opening",
       COALESCE(SUM(m.in_amt), 0)%[4]s  AS "line.in",
       COALESCE(SUM(m.out_amt), 0)%[4]s AS "line.out"
  FROM %[3]s w
  LEFT JOIN (
    SELECT t.wallet_id AS wallet_id,
           CASE WHEN `+priorPredicate+`
                THEN (CASE WHEN t.kind IN ('income','opening','adjustment_in') THEN t.amount_minor ELSE -t.amount_minor END)
                ELSE 0 END AS opening,
           CASE WHEN t.period_id = #periodID AND t.kind IN ('income','opening','adjustment_in')
                THEN t.amount_minor ELSE 0 END AS in_amt,
           CASE WHEN t.period_id = #periodID AND t.kind IN ('expense','transfer','adjustment_out')
                THEN t.amount_minor ELSE 0 END AS out_amt
      FROM %[1]s t
     WHERE `+scopePredicateT+` AND t.currency = #currency
    UNION ALL
    SELECT t.to_wallet_id,
           CASE WHEN `+priorPredicate+` THEN t.amount_minor ELSE 0 END,
           CASE WHEN t.period_id = #periodID THEN t.amount_minor ELSE 0 END,
           0
      FROM %[1]s t
     WHERE `+scopePredicateT+` AND t.currency = #currency
       AND t.kind = 'transfer' AND t.to_wallet_id IS NOT NULL
  ) m ON m.wallet_id = w.id
 WHERE w.org_id = #orgID AND w.org_id = #scopeOrg AND w.project_id = #projectID AND w.currency = #currency
 GROUP BY w.id, w.name, w.kind, w.exclude_from_total
 ORDER BY LOWER(w.name), w.id`,
		t.txn, t.periods, t.wallets, bigintCast(pg))
}

func categorySlicesSQL(t tableNames, pg bool) string {
	return fmt.Sprintf(`
SELECT t.category_id            AS "slice.category_id",
       COALESCE(c.name, '')     AS "slice.name",
       COALESCE(c.icon, '')     AS "slice.icon",
       COALESCE(c.color, '')    AS "slice.color",
       SUM(t.amount_minor)%[3]s AS "slice.amount",
       COUNT(*)%[4]s            AS "slice.count"
  FROM %[1]s t
  LEFT JOIN %[2]s c ON c.id = t.category_id AND c.org_id = t.org_id
 WHERE `+scopePredicateT+` AND t.period_id = #periodID AND t.currency = #currency AND t.kind = #kind
 GROUP BY t.category_id, c.name, c.icon, c.color
 ORDER BY SUM(t.amount_minor) DESC, COALESCE(c.name, '') ASC,
          CASE WHEN t.category_id IS NULL THEN 1 ELSE 0 END, t.category_id`,
		t.txn, t.categories, bigintCast(pg), intCast(pg))
}

func flowsSQL(t tableNames, pg bool) string {
	return fmt.Sprintf(`
SELECT t.wallet_id            AS "flow.wallet_id",
       COALESCE(w.name, '')   AS "flow.wallet_name",
       t.category_id          AS "flow.category_id",
       COALESCE(c.name, '')   AS "flow.category_name",
       SUM(t.amount_minor)%[4]s AS "flow.amount"
  FROM %[1]s t
  LEFT JOIN %[2]s w ON w.id = t.wallet_id AND w.org_id = t.org_id
  LEFT JOIN %[3]s c ON c.id = t.category_id AND c.org_id = t.org_id
 WHERE `+scopePredicateT+` AND t.period_id = #periodID AND t.currency = #currency AND t.kind = 'expense'
 GROUP BY t.wallet_id, w.name, t.category_id, c.name
 ORDER BY SUM(t.amount_minor) DESC, COALESCE(w.name, '') ASC, t.wallet_id,
          CASE WHEN t.category_id IS NULL THEN 1 ELSE 0 END, t.category_id`,
		t.txn, t.wallets, t.categories, bigintCast(pg))
}

func cashflowSQL(t tableNames, pg bool, inList string) string {
	return fmt.Sprintf(`
SELECT p.id         AS "point.period_id",
       p.name       AS "point.name",
       p.start_date AS "point.start",
       p.end_date   AS "point.end",
       COALESCE(SUM(CASE WHEN t.kind IN ('income','adjustment_in')   THEN t.amount_minor ELSE 0 END), 0)%[3]s AS "point.income",
       COALESCE(SUM(CASE WHEN t.kind IN ('expense','adjustment_out') THEN t.amount_minor ELSE 0 END), 0)%[3]s AS "point.expense"
  FROM %[2]s p
  LEFT JOIN %[1]s t
    ON t.period_id = p.id AND t.org_id = p.org_id AND t.project_id = p.project_id AND t.currency = #currency
 WHERE p.org_id = #orgID AND p.org_id = #scopeOrg AND p.project_id = #projectID AND p.id IN (%[4]s)
 GROUP BY p.id, p.name, p.start_date, p.end_date`,
		t.txn, t.periods, bigintCast(pg), inList)
}

func walletBalancesSQL(t tableNames, pg bool) string {
	return fmt.Sprintf(`
SELECT w.id                 AS "line.wallet_id",
       w.name               AS "line.name",
       w.kind               AS "line.kind",
       w.exclude_from_total AS "line.exclude",
       w.currency           AS "line.currency",
       COALESCE(SUM(m.in_amt), 0)%[3]s  AS "line.in",
       COALESCE(SUM(m.out_amt), 0)%[3]s AS "line.out"
  FROM %[2]s w
  LEFT JOIN (
    SELECT t.wallet_id AS wallet_id,
           CASE WHEN t.kind IN ('income','opening','adjustment_in') THEN t.amount_minor ELSE 0 END AS in_amt,
           CASE WHEN t.kind IN ('expense','transfer','adjustment_out') THEN t.amount_minor ELSE 0 END AS out_amt
      FROM %[1]s t
     WHERE `+scopePredicateT+`
    UNION ALL
    SELECT t.to_wallet_id, t.amount_minor, 0
      FROM %[1]s t
     WHERE `+scopePredicateT+` AND t.kind = 'transfer' AND t.to_wallet_id IS NOT NULL
  ) m ON m.wallet_id = w.id
 WHERE w.org_id = #orgID AND w.org_id = #scopeOrg AND w.project_id = #projectID
 GROUP BY w.id, w.name, w.kind, w.exclude_from_total, w.currency
 ORDER BY LOWER(w.name), w.id`,
		t.txn, t.wallets, bigintCast(pg))
}

type tokenSet struct {
	list string
	args map[string]uuid.UUID
}

func periodTokens(ids []uuid.UUID) (tokenSet, error) {
	if len(ids) > maxCashflowPeriods {
		return tokenSet{}, errTooManyPeriods
	}
	tokens := make([]string, 0, len(ids))
	args := make(map[string]uuid.UUID, len(ids))
	for i, id := range ids {
		// NOTE: go-jet substitutes named args by plain substring match, so every token is padded to one width.
		token := fmt.Sprintf("#pid%04d", i)
		tokens = append(tokens, token)
		args[token] = id
	}
	return tokenSet{list: strings.Join(tokens, ", "), args: args}, nil
}

func cashflowPoint(ref PeriodRef, income, expense int64, currency money.Currency) CashflowPoint {
	return CashflowPoint{
		Period:  ref,
		Income:  money.New(income, currency),
		Expense: money.New(expense, currency),
		Net:     money.New(income-expense, currency),
	}
}

// bigintCast keeps Postgres SUM() out of numeric; SQLite needs no cast at all.
func bigintCast(pg bool) string {
	if pg {
		return "::bigint"
	}
	return ""
}

func intCast(pg bool) string {
	if pg {
		return "::int"
	}
	return ""
}
