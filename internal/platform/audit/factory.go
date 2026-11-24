package audit

import (
	"time"

	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

// NewRecorder selects the store for the configured driver.
func NewRecorder(cfg db.DBConfig, pool db.Pool, pc *tenant.PgConn) *Recorder {
	var s store = newSQLiteStore(pool.W, cfg.TablePrefix)
	if cfg.Driver == db.DriverPostgres {
		s = newPostgresStore(pc, cfg.Schema, cfg.TablePrefix)
	}
	return &Recorder{store: s, now: time.Now}
}
