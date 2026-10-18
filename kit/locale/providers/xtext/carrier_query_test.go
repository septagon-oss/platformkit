package xtext_test

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// The question the pseudo-locale gate asks — does this language answer this key with
// copy of its own — has to be askable by a caller holding what `Load` returns and
// nothing more. A method on a type the package keeps to itself is not askable, so the
// query carries a name, and every catalogue this function composes answers it.
func TestTheComposedCatalogueAnswersTheNamedCarrierQuery(t *testing.T) {
	var catalogue xtext.Catalog = xtext.Load("en", xtext.Source{
		Name: "task",
		FS: fstest.MapFS{
			"en.json": &fstest.MapFile{Data: []byte(`{"task.open": {"translation": "Open"}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(
				`{"task.open": {"translation": "Aberto"}}`)},
		},
	})
	carrier, ok := catalogue.(xtext.Carrier)
	if !ok {
		t.Fatal("the catalogue Load composed does not answer the query xtext names")
	}
	if !carrier.Carries("pt-PT", "task.open") {
		t.Error(`Carries("pt-PT", "task.open") = false; pt-PT.json answers that key`)
	}
	if !carrier.Carries("en", "task.open") {
		t.Error(`Carries("en", "task.open") = false; the source file answers that key`)
	}
	if carrier.Carries("pt-PT", "task.closed") {
		t.Error(`Carries answered for "task.closed", which no file carries`)
	}
	if carrier.Carries("de-DE", "task.open") {
		t.Error(`Carries("de-DE", …) = true; nobody loaded a German file, which carries nothing`)
	}
}
