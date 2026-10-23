package filetest

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Alt text is guarded by the revision the editor was shown.
func reviseAltText(t *testing.T, f Fixture) {
	lib := library(t, f)
	pic := stored(t, f, hello)
	for _, claim := range [][2]any{{"", false}, {"a red door", true}} {
		if _, err := lib.SetAlt(f.Ctx, f.Tx, pic.ID, claim[0].(string), claim[1].(bool), pic.UpdatedAt); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("alt %q decorative=%v = %v, want ErrInvalid", claim[0], claim[1], err)
		}
	}
	// Every refusal above wrote nothing and published nothing: the clock on
	// the row is the one the case was handed.
	if still, _ := read(t, f, pic.ID, false); !still.UpdatedAt.Equal(pic.UpdatedAt) {
		t.Errorf("a refused caption moved updatedAt to %s, want %s", still.UpdatedAt, pic.UpdatedAt)
	}
	var written *contracts.File
	f.one(t, "declaring an image decorative", contracts.EventAltered, func() {
		out, err := lib.SetAlt(f.Ctx, f.Tx, pic.ID, "  ", true, pic.UpdatedAt)
		if err != nil {
			t.Fatalf("a decorative image, which says nothing about itself: %v", err)
		}
		written = out
	})
	if !written.Decorative || written.Alt != "" {
		t.Errorf("the write left alt %q decorative %v, want the empty claim", written.Alt, written.Decorative)
	}
	stale := written.UpdatedAt
	f.one(t, "captioning it", contracts.EventAltered, func() {
		out, err := lib.SetAlt(f.Ctx, f.Tx, pic.ID, "  A red door, seen from the street  ", false, written.UpdatedAt)
		if err != nil {
			t.Fatalf("an alt text: %v", err)
		}
		written = out
	})
	if written.Alt != "A red door, seen from the street" {
		t.Errorf("the caption is %q, want the words with their padding removed", written.Alt)
	}
	before := len(f.Published())
	if _, err := lib.SetAlt(f.Ctx, f.Tx, pic.ID, "somebody else's sentence", false, stale); !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a caption against the instant the row has passed = %v, want ErrConflict", err)
	}
	if _, err := lib.SetAlt(f.Ctx, f.Tx, pic.ID, "no revision at all", false, time.Time{}); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("a write that names no revision = %v, want ErrInvalid: this module has no last-write-wins command", err)
	}
	if got := f.Published()[before:]; len(got) != 0 {
		t.Errorf("the refused captions published %v, want nothing", got)
	}
	if still, _ := read(t, f, pic.ID, false); still.Alt != "A red door, seen from the street" {
		t.Errorf("the refused captions left alt %q, want the accepted one undisturbed", still.Alt)
	}
}

// The grid combines name, media family and unused-file filters.
func filterLibrary(t *testing.T, f Fixture) {
	lib := library(t, f)
	frame, err := f.Service.Upload(f.Ctx, f.open, asImage(t, "Foto 2026.png", "image/png", pngFrame(t, 8, 6)))
	if err != nil {
		t.Fatalf("an image upload: %v", err)
	}
	drawing, err := f.Service.Upload(f.Ctx, f.open, upload("drawing.svg", "image/svg+xml", contracts.VisibilityPrivate, "<svg xmlns=\"http://www.w3.org/2000/svg\"/>"))
	if err != nil {
		t.Fatalf("a drawing: %v", err)
	}
	notes := stored(t, f, hello)
	names := func(q contracts.LibraryQuery) []string {
		t.Helper()
		rows, total, err := lib.Search(f.Ctx, f.Tx, q)
		if err != nil {
			t.Fatalf("Search(%+v): %v", q, err)
		}
		if total != int64(len(rows)) {
			t.Fatalf("Search(%+v) returned %d rows of a total %d", q, len(rows), total)
		}
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, row.Name)
		}
		return got
	}
	// Names are compared as a set: three uploads can share one microsecond,
	// and which of them the tie-break puts first is the pagination's
	// business, not the filter's.
	want := func(names ...string) []string { slices.Sort(names); return names }
	got := func(q contracts.LibraryQuery) []string { out := names(q); slices.Sort(out); return out }
	switch {
	case !slices.Equal(got(contracts.LibraryQuery{Family: contracts.FamilyImage}), want(frame.Name)):
		t.Errorf("type=image gave %v, want the one picture: a drawing nobody measured is not one", got(contracts.LibraryQuery{Family: contracts.FamilyImage}))
	case len(names(contracts.LibraryQuery{Family: contracts.FamilyVideo})) != 0:
		t.Errorf("type=video gave %v, want nothing", names(contracts.LibraryQuery{Family: contracts.FamilyVideo}))
	case !slices.Equal(got(contracts.LibraryQuery{Family: contracts.FamilyDocument}), want(drawing.Name, notes.Name)):
		t.Errorf("type=document gave %v, want the drawing and the note", got(contracts.LibraryQuery{Family: contracts.FamilyDocument}))
	case !slices.Equal(got(contracts.LibraryQuery{Name: "FOTO"}), want(frame.Name)):
		t.Errorf(`q=FOTO gave %v, want the photograph: a name is searched case-insensitively`, got(contracts.LibraryQuery{Name: "FOTO"}))
	case !slices.Equal(got(contracts.LibraryQuery{}), want(frame.Name, drawing.Name, notes.Name)):
		t.Errorf("the unfiltered grid gave %v, want all three", got(contracts.LibraryQuery{}))
	}
	// The unused filter reads the ledger's stamp, and nothing else: an
	// upload the ledger has never mentioned is not "unused", because nothing
	// measured when it stopped being shown.
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{frame.ID}); err != nil {
		t.Fatalf("a page that shows the picture: %v", err)
	}
	if got := names(contracts.LibraryQuery{Unused: true}); len(got) != 0 {
		t.Errorf("unused=1 with the picture shown gave %v, want nothing", got)
	}
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, nil); err != nil {
		t.Fatalf("the edit that dropped it: %v", err)
	}
	if got := names(contracts.LibraryQuery{Unused: true}); !slices.Equal(got, []string{frame.Name}) {
		t.Errorf("unused=1 after the last use ended gave %v, want the picture the ledger stamped", got)
	}
	for _, bad := range []contracts.LibraryQuery{
		{Name: strings.Repeat("q", contracts.MaxQuery+1)},
		{Family: "pictures"},
		{Sort: "pixels"},
	} {
		if _, _, err := lib.Search(f.Ctx, f.Tx, bad); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Search(%+v) = %v, want ErrInvalid naming what it accepts", bad, err)
		}
	}
}
