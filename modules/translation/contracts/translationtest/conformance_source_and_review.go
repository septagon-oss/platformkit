package translationtest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

func markChangedSource(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f.Source.Put(id, db.Now().Add(hour), Source("About us", firstParagraph, secondParagraph))
	if got := f.read(t, id, false)[FieldBody].Status; got != "" {
		t.Errorf("a source saved with the same words reports %q; the digest decides and the digest did not move", got)
	}
	f.Source.Put(id, db.Now().Add(hour), Source("About us", "We build software for banks.", secondParagraph))
	fields := f.read(t, id, false)
	if fields[FieldBody].Status != rest.FallbackOutdated {
		t.Errorf("body reports %q after its source changed, want %q", fields[FieldBody].Status, rest.FallbackOutdated)
	}
	if !strings.HasPrefix(fields[FieldBody].Value, "Constru") {
		t.Errorf("an outdated translation stopped being served: %q — stale is labelled, not hidden", fields[FieldBody].Value)
	}
}

func preserveOriginalSource(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, moved := range []string{"We build software for banks.", "We build software for banks and insurers."} {
		f.Source.Put(id, db.Now().Add(hour), Source("About us", moved, secondParagraph))
	}
	var stored *Row
	rows := f.Rows()
	for i := range rows {
		if rows[i].Field == FieldBody {
			stored = &rows[i]
		}
	}
	if stored == nil {
		t.Fatal("the body's translation is gone")
	}
	if !strings.Contains(stored.SourceText, firstParagraph) || strings.Contains(stored.SourceText, "banks") {
		t.Errorf("the source copy was re-based onto the newest source (%q); the paragraph the reviewer was told to look at is gone with it",
			stored.SourceText)
	}
}

func reviewUpdatedTranslation(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f.Source.Put(id, db.Now().Add(hour), Source("About us", "We build software for banks.", secondParagraph))
	f.silent(t, "the review of a stale translation", func() {
		if err := f.review(t, id); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("reviewing a stale translation answered %v, want a refusal naming the source", err)
		}
	})
	if err := f.save(id, "Construímos software para bancos.\n\n"+secondParagraph, "Sobre nós", 1, 1); err != nil {
		t.Fatalf("the corrected save: %v", err)
	}
	// The corrected save is a person's own text, and a person's text is
	// reviewed by being typed: the review that follows succeeds and
	// writes nothing, because the only thing a review can record is that
	// somebody accepted a machine draft.
	f.silent(t, "the review of a human translation", func() {
		if err := f.review(t, id); err != nil {
			t.Fatalf("Review after the fix: %v", err)
		}
	})
	if got := f.read(t, id, false)[FieldBody].Status; got != "" {
		t.Errorf("after a fix the body still reports %q", got)
	}
}

func hashStoredSource(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, "Construímos software.", "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := f.save(id, "Também.", "Também", 1, 1); err != nil {
		t.Fatalf("the second save: %v", err)
	}
	rows := f.Rows()
	if len(rows) != 2 {
		t.Fatalf("the store holds %d rows, want the two fields", len(rows))
	}
	for _, r := range rows {
		hash, err := contracts.Hash(r.SourceText, r.Field == FieldBody)
		if err != nil {
			t.Fatalf("hashing %s: %v", r.Field, err)
		}
		if hash != r.SourceHash {
			t.Errorf("%s stores a source_hash that is not the digest of its own source_text", r.Field)
		}
	}
}
