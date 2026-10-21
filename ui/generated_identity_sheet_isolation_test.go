package ui_test

import (
	"bytes"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// Each composition keeps both modes of its own generated identity, even when
// another client's sheet is composed and changed in the same process.
func TestGeneratedIdentitySheetsKeepTheirOwnTokensAndStorage(t *testing.T) {
	var sheets []ui.Sheet
	for _, seed := range []design.Seed{
		{Sector: "learning", Name: "First identity", Brand: "#f0b978"},
		{Sector: "publishing", Name: "Second identity", Brand: "#2459ab"},
	} {
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatal(err)
		}
		sheet := ui.Compose(pair)
		for _, theme := range pair.Both() {
			for _, token := range theme.Tokens() {
				if !bytes.Contains(sheet.Body, []byte(token.Name+": "+token.Value+";")) {
					t.Errorf("%s/%s: composed sheet lost token %s", seed.Name, theme.Name, token.Name)
				}
			}
		}
		sheets = append(sheets, sheet)
	}
	if bytes.Equal(sheets[0].Body, sheets[1].Body) || sheets[0].Fingerprint == sheets[1].Fingerprint {
		t.Fatal("different generated identities share their sheet or asset fingerprint")
	}
	first := bytes.Clone(sheets[0].Body)
	if len(sheets[1].Body) == 0 {
		t.Fatal("second composition returned an empty sheet")
	}
	sheets[1].Body[0] ^= 1
	if !bytes.Equal(first, sheets[0].Body) {
		t.Fatal("changing the second client's sheet changed the first client's sheet")
	}
}
