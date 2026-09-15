package boot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
)

func TestMigratorConfig_RoleAlwaysFromMigratorRole(t *testing.T) {
	cases := []struct {
		name        string
		migratorDSN string
		dbRole      string
		migRole     string
		wantDSN     string
		wantRole    string
	}{
		{"separate dsn uses migrator role", "postgres://mig@h/db", "yasaku_service", "yasaku_owner", "postgres://mig@h/db", "yasaku_owner"},
		{"single dsn still uses migrator role", "", "yasaku_service", "yasaku_owner", "postgres://app@h/db", "yasaku_owner"},
		{"no migrator role means none", "", "yasaku_service", "", "postgres://app@h/db", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.DB.Driver = "postgres"
			cfg.DB.DSN = "postgres://app@h/db"
			cfg.DB.Role = tc.dbRole
			cfg.DB.Migrator.DSN = tc.migratorDSN
			cfg.DB.Migrator.Role = tc.migRole

			got := MigratorDBConfig(cfg)

			require.Equal(t, tc.wantRole, got.Role, "db.role must never leak into migrations")
			require.Equal(t, tc.wantDSN, got.DSN)
			require.Equal(t, 1, got.MaxOpenConns, "goose must run its sequence on one session")
			require.Equal(t, 1, got.MaxIdleConns)
			require.Equal(t, tc.dbRole, cfg.DB.Role, "shaping the migrator config must not mutate the runtime config")
		})
	}
}
