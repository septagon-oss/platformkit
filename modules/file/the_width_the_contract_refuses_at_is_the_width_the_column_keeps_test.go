package file_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheWidthTheContractRefusesAtIsTheWidthTheColumnKeeps asks that every
// length this module refuses at equals the width the column keeps, read from
// the live schema after this module's migrations. Each limit exists to turn a
// value the column cannot hold into a 422 that names the field rather than a
// constraint violation the caller reads as a 500, which stops being true the
// moment the two figures differ: a narrower column answers the 500 again and a
// wider one refuses a value the table could have kept. The widths are read
// from the database and not from the SQL text, so a later ALTER of a column
// moves this case with the schema.
func TestTheWidthTheContractRefusesAtIsTheWidthTheColumnKeeps(t *testing.T) {
	admin, _ := dbtest.Schema(t, file.Migrations)
	want := map[string]int{
		"name":         contracts.MaxName,
		"content_type": contracts.MaxContentType,
		"alt":          contracts.MaxAlt,
	}
	rows, err := admin.QueryContext(t.Context(), `
		SELECT column_name, character_maximum_length
		  FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'files'
		   AND column_name IN ('name', 'content_type', 'alt')`)
	if err != nil {
		t.Fatalf("reading the widths files keeps: %v", err)
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var column string
		var width int
		if err := rows.Scan(&column, &width); err != nil {
			t.Fatalf("reading one column width: %v", err)
		}
		seen[column] = width
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the widths files keeps: %v", err)
	}
	for column, refused := range want {
		width, ok := seen[column]
		if !ok {
			t.Errorf("files carries no column %s, which the limit of %d characters has no width to agree with", column, refused)
			continue
		}
		if width != refused {
			t.Errorf("files.%s keeps %d characters and the contract refuses past %d: one limit is two figures", column, width, refused)
		}
	}
}
