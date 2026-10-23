package translationtest

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

func describeTranslationEvents(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	f.nTimes(t, "the save", 2, func() {
		if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
			t.Fatalf("Save: %v", err)
		}
	})
	events := f.Payloads()
	if len(events) != 2 {
		t.Fatalf("a save of two fields published %d events, want one per field", len(events))
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Module != TestModule || ev.Entity != TestEntity || ev.Locale != PT || ev.RecordID != id {
			t.Errorf("an event names %s.%s/%s of %s, want this record", ev.Module, ev.Entity, ev.Locale, ev.RecordID)
		}
		if ev.Revision != 1 || ev.Origin != rest.OriginHuman || ev.SourceHash == "" {
			t.Errorf("%s was announced at revision %d from %q with hash %q", ev.Field, ev.Revision, ev.Origin, ev.SourceHash)
		}
		seen[ev.Field] = true
	}
	if !seen[FieldBody] || !seen[FieldTitle] {
		t.Errorf("the fields announced were %v", sortedKeys(seen))
	}
}

func forgetAllLocales(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Two events: the record had both fields translated, and each is a
	// field that stopped existing. One event for the pair would leave a
	// subscriber unable to say which text went away.
	f.nTimes(t, "forgetting the record", 2, func() {
		if err := f.Service.ForgetRecord(f.Ctx, f.Tx, TestModule, TestEntity, id); err != nil {
			t.Fatalf("ForgetRecord: %v", err)
		}
	})
	if rows := f.Rows(); len(rows) != 0 {
		t.Errorf("the record is gone and %d of its translations remain", len(rows))
	}
	if fields := f.read(t, id, false); len(fields) != 0 {
		t.Errorf("a forgotten record still answers %d translated fields", len(fields))
	}
}

func removeTranslation(t *testing.T, f Fixture) {
	id, _ := f.page("About us", firstParagraph, secondParagraph)
	if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f.once(t, "untranslating the body", func() {
		if err := f.Service.Untranslate(f.Ctx, f.Tx, rest.ReviewQuery{
			Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id, Fields: []string{FieldBody},
		}); err != nil {
			t.Fatalf("Untranslate: %v", err)
		}
	})
	events := f.Payloads()
	last := events[len(events)-1]
	if last.Field != FieldBody || last.Status != rest.FallbackRemoved {
		t.Errorf("the removal was announced as %s/%s, want %s removed", last.Field, last.Status, FieldBody)
	}
	if fields := f.read(t, id, false); len(fields) != 1 || fields[FieldTitle].Value == "" {
		t.Errorf("after removing the body, %d fields remain: %v", len(fields), fields)
	}
}
