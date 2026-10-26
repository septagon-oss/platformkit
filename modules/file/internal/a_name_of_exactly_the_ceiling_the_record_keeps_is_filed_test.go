package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestANameOfExactlyTheCeilingTheRecordKeepsIsFiled asks the half of the name
// rule that nothing else asks: a name of exactly contracts.MaxName characters is
// filed. The refusal one character past the ceiling is answered elsewhere, and a
// ceiling that refused at its own count — or counted bytes where its message
// counts characters — refuses this upload and stays green there, because a name
// spelled in two-byte letters is over the count under either reading. The pair of
// answers is what holds the line: files.name is varchar(255), which Postgres
// measures in characters, and a tenant that does not write English spells a
// 255-character name in more bytes than that. Measured in bytes the module would
// refuse a name its own error message says is allowed.
func TestANameOfExactlyTheCeilingTheRecordKeepsIsFiled(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	svc := internal.NewService(internal.NewLocal(dir), 16<<20, 0, 0)

	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	// The ceiling exactly, in characters: 510 bytes to a byte-counting guard.
	name := strings.Repeat("é", contracts.MaxName)

	ctx := tenancy.WithTenant(t.Context(), acme)
	var got string
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: name, ContentType: "image/jpeg",
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		if f != nil {
			got = f.Name
		}
		return err
	})
	if err != nil {
		t.Fatalf("an upload of a %d-character name (%d bytes) = %v; the ceiling is %d characters",
			contracts.MaxName, len(name), err, contracts.MaxName)
	}
	if got != name || utf8.RuneCountInString(got) != contracts.MaxName {
		t.Errorf("the row the upload filed is named %q (%d characters), want the %d-character name it was given",
			got, utf8.RuneCountInString(got), contracts.MaxName)
	}
	if keys := keysUnder(t, dir); len(keys) != 1 {
		t.Errorf("the upload left %d objects in the store (%v), want the one its row names", len(keys), keys)
	}
}
