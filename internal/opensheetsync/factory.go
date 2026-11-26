package opensheetsync

import (
	"altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

// NewStore dispatches to the driver-specific Store implementation.
func NewStore(cfg db.DBConfig, pool db.Pool, pc *tenant.PgConn) Store {
	if cfg.Driver == db.DriverPostgres {
		return newPostgresStore(pc, cfg.Schema, cfg.TablePrefix)
	}
	return newSQLiteStore(pool.W, cfg.TablePrefix)
}

// NewGrantStore dispatches to the driver-specific GrantStore implementation.
func NewGrantStore(cfg db.DBConfig, pool db.Pool, pc *tenant.PgConn) GrantStore {
	if cfg.Driver == db.DriverPostgres {
		return &pgGrantStore{pc: pc, grants: pgent.NewOpensheetGrants(cfg.Schema, cfg.TablePrefix)}
	}
	return &sqliteGrantStore{db: pool.W, grants: sqliteent.NewOpensheetGrants(cfg.TablePrefix)}
}
