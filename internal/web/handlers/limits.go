package handlers

import (
	"context"
	"errors"
	"net/http"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/limits"
)

func (d Deps) limitMessage(ctx context.Context, err error) (string, bool) {
	var re *limits.ReachedError
	if !errors.As(err, &re) {
		return "", false
	}
	return limits.Message(i18n.From(ctx).String(), d.Cfg.Mode, re, d.Cfg.Compliance.ContactEmail), true
}

func orgCreateQuiet(err error) bool {
	re, ok := errors.AsType[*limits.ReachedError](err)
	return ok && re.Max == 1
}

func (d Deps) limitBlocked(r *http.Request, err error, logMsg string) (msg, code string) { //nolint:nonamedreturns // message and code differ in role
	if err == nil {
		return "", ""
	}
	m, ok := d.limitMessage(r.Context(), err)
	if !ok {
		d.LogErr(logMsg, err)
		return "", ""
	}
	return m, ErrorRef(err)
}
