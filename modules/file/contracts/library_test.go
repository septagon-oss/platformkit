package contracts

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// The four decisions below are the library's whole grammar, and they are pure
// functions for the reason the image pass and the use ledger already are: the
// SQL service and filetest.Fake run the same one instead of each keeping an
// copy. What a suite cannot check through a fake — an order, a ceiling, a stamp
// — is checked here, where the decision itself lives.

func TestTheFamilyFilterTrustsTheFrameAndNotTheTypePrefix(t *testing.T) {
	cases := []struct {
		contentType string
		width       int
		want        string
	}{
		{"image/png", 800, FamilyImage},
		{"Image/JPEG; charset=binary", 1, FamilyImage},
		{"image/webp", 640, FamilyImage},
		// A drawing nobody measured, and a file whose declared type lied about
		// itself: neither is a photograph on the grid, and a filter that trusted
		// the "image/" prefix would put both there with a broken tile each.
		{"image/svg+xml", 0, FamilyDocument},
		{"image/png", 0, FamilyDocument},
		{"image/png", -1, FamilyDocument},
		{"video/mp4", 0, FamilyVideo},
		{"video/webm", 1920, FamilyVideo},
		// A video this deployment would not render inline is a document: the
		// family is what a person would call it, and the type filter lists files.
		{"video/quicktime", 0, FamilyDocument},
		{"application/pdf", 0, FamilyDocument},
		{"", 0, FamilyDocument},
		{"not a media type", 100, FamilyDocument},
	}
	for _, c := range cases {
		if got := MediaFamily(c.contentType, c.width); got != c.want {
			t.Errorf("MediaFamily(%q, %d) = %s, want %s", c.contentType, c.width, got, c.want)
		}
	}
	// The sets the filter asks the database for are read off the renderable list
	// rather than written beside it, or the grid and the download policy drift.
	for _, ct := range append(slices.Clone(ImageTypes), VideoTypes...) {
		if !Renderable(ct) {
			t.Errorf("%s is in a family the library can filter by but is not renderable", ct)
		}
	}
	if slices.Contains(ImageTypes, "image/svg+xml") {
		t.Error("image/svg+xml is an image family member; nothing decodes it and a tile for it is a broken image")
	}
}

func TestTheQuotaMeterAddsUpWithoutDividingByZero(t *testing.T) {
	full := Quota{Used: 900, Limit: 1000}
	if full.Left() != 100 || full.Full() || full.Fraction() <= 0 || full.Fraction() >= 1 {
		t.Errorf("900 of 1000: left %d, full %v, fraction %v", full.Left(), full.Full(), full.Fraction())
	}
	// Past the ceiling the remainder is zero and not negative: a meter that shows
	// -4 MB is a gauge that has stopped meaning anything, and the refusal's own
	// message is where the overage belongs.
	over := Quota{Used: 1400, Limit: 1000}
	if over.Left() != 0 || !over.Full() || over.Fraction() != 1 {
		t.Errorf("1400 of 1000: left %d, full %v, fraction %v, want 0, true, 1", over.Left(), over.Full(), over.Fraction())
	}
	// An installation that set no ceiling has no bar to draw and no remainder to
	// promise. -1 is the answer that says "there is no number here" without
	// pretending to be one.
	open := Quota{Used: 5 << 30, Limit: 0, Unlimited: true}
	if open.Left() != -1 || open.Full() || open.Fraction() != 0 {
		t.Errorf("no ceiling: left %d, full %v, fraction %v, want -1, false, 0", open.Left(), open.Full(), open.Fraction())
	}
	if empty := (Quota{Limit: 1000}); empty.Fraction() != 0 || empty.Left() != 1000 || empty.Full() {
		t.Errorf("a tenant with no rows: fraction %v, left %d, full %v", empty.Fraction(), empty.Left(), empty.Full())
	}
}

func TestTheGridRefusesASortATypeAndAQueryItDoesNotOffer(t *testing.T) {
	for _, spec := range Sorts {
		field, _, err := LibrarySort(spec)
		if err != nil {
			t.Errorf("LibrarySort(%q): %v, want the allow list to accept its own list", spec, err)
		}
		if field == "" || strings.Contains(field, "-") {
			t.Errorf("LibrarySort(%q) named field %q, want a field and no sign", spec, field)
		}
	}
	field, desc, err := LibrarySort("")
	if err != nil || field != "CreatedAt" || !desc {
		t.Errorf("the empty sort is %q desc=%v (%v), want newest first", field, desc, err)
	}
	for _, bad := range []string{"pixels", "createdAt,", "created_at", "name ASC", "--name", "id"} {
		if _, _, err := LibrarySort(bad); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("LibrarySort(%q) = %v, want ErrInvalid", bad, err)
		} else if !strings.Contains(err.Error(), SortNewest) {
			t.Errorf("LibrarySort(%q) refused with %q, which does not name the orders it accepts", bad, err)
		}
	}

	// Normalize is what both doors run: the defaults, the trims, and the same
	// refusals naming the accepted values.
	q, err := LibraryQuery{Name: "  cover  ", Family: " IMAGE ", Sort: " name "}.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if q.Name != "cover" || q.Family != FamilyImage || q.Limit != DefaultLibraryPageSize {
		t.Errorf("Normalize gave %+v, want the trimmed query with the default page", q)
	}
	if q, err := (LibraryQuery{Family: "all"}).Normalize(); err != nil || q.Family != "" {
		t.Errorf(`type=all is %+v (%v), want no filter at all`, q, err)
	}
	if q, err := (LibraryQuery{Offset: -4}).Normalize(); err != nil || q.Offset != 0 {
		t.Errorf("a negative offset is %+v (%v), want the first page", q, err)
	}
	for _, bad := range []LibraryQuery{
		{Name: strings.Repeat("q", MaxQuery+1)},
		{Family: "pictures"},
		{Sort: "pixels"},
	} {
		if _, err := bad.Normalize(); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Normalize(%+v) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestACaptionIsWordsOrTheClaimThatThereAreNone(t *testing.T) {
	if err := AltClaim("A red door", false); err != nil {
		t.Errorf("words: %v", err)
	}
	if err := AltClaim("", true); err != nil {
		t.Errorf("the claim that there is nothing to say: %v", err)
	}
	for _, claim := range []struct {
		alt        string
		decorative bool
	}{
		{"", false},                            // neither: an image that says nothing about itself
		{"a red door", true},                   // both: a contradiction a reader would arbitrate
		{"   \n\t ", false},                    // neither, wearing whitespace
		{strings.Repeat("é", MaxAlt+1), false}, // characters, not bytes
	} {
		if err := AltClaim(claim.alt, claim.decorative); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("AltClaim(%q, %v) = %v, want ErrInvalid", claim.alt, claim.decorative, err)
		}
	}
	// The limit counts what a person typed, and 1000 accented characters fit in
	// the column that is 1000 characters wide because Postgres counts the same way.
	if err := AltClaim(strings.Repeat("é", MaxAlt), false); err != nil {
		t.Errorf("%d characters of alt text: %v", MaxAlt, err)
	}
}
