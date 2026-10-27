package internal

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheEssenceTheGridFiltersByIsTheEssenceTheContractNames puts the library's
// three type filters on one ground: essenceSQL, which SQL reads the content_type
// column through, and contracts.Essence, which Go reads the stored value through,
// are two spellings of one rule. A row the two readings disagree about is a row a
// person finds under one type filter and not another — the grid's own answer and
// the tile's own label disagreeing on one page — so the agreement is asked here
// over the media types an upload can declare, rather than over the three a page
// can afford to file.
//
// Each answer is the expression as the package holds it and the function as the
// contract holds it: a case that restated either side would stay green while the
// two drifted apart. The forms below are the padding, the casing and the
// parameters of the whitespace and case Postgres' btrim and Go's TrimSpace both
// remove; btrim removes spaces alone where TrimSpace removes every whitespace, so a
// tab is the one padding the two readings do not share, and the forms below are the
// ones they do.
func TestTheEssenceTheGridFiltersByIsTheEssenceTheContractNames(t *testing.T) {
	admin, _ := dbtest.Schema(t)
	const probe = "essence_probe"
	if _, err := admin.ExecContext(t.Context(), `CREATE TABLE `+probe+` (content_type text not null)`); err != nil {
		t.Fatalf("the probe table: %v", err)
	}
	declared := []string{
		"image/jpeg", "IMAGE/JPEG", " image/jpeg ; charset=utf-8", "image/jpeg;charset=binary",
		"video/mp4 ;charset=utf-8", "Video/MP4", " video/webm ;type=streaming",
		"text/plain ;charset=utf-8", "TEXT/CSV", "application/pdf; x=1",
		"image/svg+xml", "image/svg+xml ;charset=utf-8",
	}
	for _, one := range declared {
		if _, err := admin.ExecContext(t.Context(), `INSERT INTO `+probe+` (content_type) VALUES ($1)`, one); err != nil {
			t.Fatalf("filing %q: %v", one, err)
		}
	}
	rows, err := admin.QueryContext(t.Context(), `SELECT content_type, `+essenceSQL+` FROM `+probe)
	if err != nil {
		t.Fatalf("the grid's expression over the probe rows: %v", err)
	}
	defer rows.Close()
	asked := 0
	for rows.Next() {
		var stored, grid string
		if err := rows.Scan(&stored, &grid); err != nil {
			t.Fatalf("reading the probe: %v", err)
		}
		asked++
		if want := contracts.Essence(stored); grid != want {
			t.Errorf("%q: the grid's SQL reads %q where the contract reads %q — the three type filters rest on one answer",
				stored, grid, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the probe's rows: %v", err)
	}
	if asked != len(declared) {
		t.Errorf("the probe answered %d of the %d declared media types", asked, len(declared))
	}
}
