package handlers

import (
	"context"
	"errors"

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
