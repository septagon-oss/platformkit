package translationtest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
)

func readEmptyTranslations(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if fields := f.read(t, id, false); len(fields) != 0 {
		t.Errorf("Translated answered %d fields of a record with no translation", len(fields))
	}
}

func saveTranslation(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fields := f.read(t, id, false)
	if fields[FieldBody].Value != translatedBody {
		t.Errorf("body is %q, want the Portuguese that was written", fields[FieldBody].Value)
	}
	if fields[FieldTitle].Value != "Sobre nós" {
		t.Errorf("title is %q", fields[FieldTitle].Value)
	}
	if fields[FieldBody].Status != "" || fields[FieldTitle].Status != "" {
		t.Errorf("a complete translation reports %q and %q; a field with nothing wrong with it is absent, not labelled complete",
			fields[FieldBody].Status, fields[FieldTitle].Status)
	}
	for _, row := range f.Rows() {
		if row.Locale != PT {
			t.Errorf("a row landed in %q, which nobody asked to write", row.Locale)
		}
	}
}

func repeatTranslation(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.nTimes(t, "the first save", 2, func() {
		if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
			t.Fatalf("Save: %v", err)
		}
	})
	f.silent(t, "the second save", func() {
		if err := f.save(id, translatedBody, "Sobre nós", 1, 1); err != nil {
			t.Fatalf("Save of the same text: %v", err)
		}
	})
	for _, row := range f.Rows() {
		if row.Revision != 1 {
			t.Errorf("%s sits at revision %d after a save that changed nothing; a revision a form cannot trust is worse than none",
				row.Field, row.Revision)
		}
	}
}

func refuseStaleRevision(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, "Primeira.", "Primeiro", 0, 0); err != nil {
		t.Fatalf("the first translator's save: %v", err)
	}
	f.silent(t, "the refused save", func() {
		err := f.save(id, "Perdedora.", "Perdedor", 99, 99)
		if !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("a stale revision answered %v, want crud.ErrConflict", err)
		}
	})
	if got := f.read(t, id, false)[FieldBody].Value; got != "Primeira." {
		t.Errorf("the loser overwrote the winner: body is %q", got)
	}
}

func refuseEmptyTranslation(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.silent(t, "the blank save", func() {
		err := f.save(id, "   ", "Sobre nós", 0, 0)
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("a blank body answered %v, want crud.ErrInvalid", err)
		}
	})
	if rows := f.Rows(); len(rows) != 0 {
		t.Errorf("a refused save left %d rows behind", len(rows))
	}
}
