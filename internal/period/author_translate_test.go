package period

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestTranslateSQLiteError_KeyAuthorCheckIsAuthorMissing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE x (a TEXT, b TEXT, CONSTRAINT x_period_closings_author_one CHECK ((a IS NULL) <> (b IS NULL)))`); err != nil {
		t.Fatal(err)
	}
	_, execErr := db.Exec(`INSERT INTO x (a, b) VALUES (NULL, NULL)`)
	if execErr == nil {
		t.Fatal("the CHECK must refuse a closing with no author")
	}
	if got := translateSQLiteError(execErr, nil); !IsAuthorMissingError(got) {
		t.Fatalf("translateSQLiteError(%v) = %v, want AuthorMissingError", execErr, got)
	}
}
