package limits

import "altalune.id/yasaku/internal/platform/db"

// NewStore selects the Store implementation for the configured driver.
func NewStore(cfg db.DBConfig, pool db.Pool) Store {
	if cfg.Driver == db.DriverPostgres {
		return newPostgresStore(pool.W, cfg.Schema, cfg.TablePrefix)
	}
	return newSQLiteStore(pool.W, cfg.TablePrefix)
}
