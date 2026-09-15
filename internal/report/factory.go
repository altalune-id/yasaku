package report

import (
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

// NewReader dispatches to the driver-specific Reader implementation.
func NewReader(cfg db.DBConfig, pool db.Pool, pc *tenant.PgConn) Reader {
	if cfg.Driver == db.DriverPostgres {
		return newPostgresReader(pool, pc, cfg.Schema, cfg.TablePrefix)
	}
	return newSQLiteReader(pool.W, cfg.TablePrefix)
}
