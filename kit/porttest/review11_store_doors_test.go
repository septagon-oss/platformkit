package porttest

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Review 11 (the last of the three-plus reviews decision 0039 names): no HIGH found, so this file
// pins the most fragile promise I checked rather than witnessing a defect.
//
// 9f15dc6 cured review 10's MEDIUM by making a Store hand out a row that shares no storage with the
// row it holds, and its doc comments make that promise at three doors: `Get` ("the row that comes
// back is the tenant's row and nobody else's"), `All` ("each of them a copy on the same terms as
// Get's, so a fake never iterates a Go map and a list case cannot pass by luck") and `Find`, which
// reads through `All`. The witnesses committed with the cure walk `Put` and `Get` — and `All` is
// the door a list reads through, which is how a fake reaches a row it then hands a command.
//
// Measured, not assumed: `Get` hands out the stored row instead of a copy and three tests fail
// (TestTheRowAStoreHandsOutSharesNoStorageWithTheRowItHolds, TestADoThatWroteThroughAHeaderKeepsWhatItCommitted,
// TestADoThatRefusesAnUndeclaredEventLeavesTheReferencedFieldsAlone). `All` hands out the stored row
// instead of a copy and the whole repository is green — kit/porttest, tasktest, contenttest,
// sitetest and the three internal suites all report ok at that mutant. So the promise at that door
// rests on nothing but the sentence. This file is the sentence's witness: it asks every door of one
// store, through the reading the fix keeps (`Get`), never through anything the defect prints.
//
// It passes as the code stands. It fails the moment any door hands out the stored row, and a door
// that returns a row of its own invention fails too, because the copy is asked to have kept what
// the row is made of.

// doorRow is a row with a header in each of the three shapes: an element of a slice, a key of a
// map, the value behind a pointer, and a slice one level deeper inside a struct element.
type doorRow struct {
	id       uuid.UUID
	Stamped  time.Time
	Title    string
	Labels   []string
	Notes    map[string]string
	Owner    *string
	Sections []doorSection
	Nil      []string
}

type doorSection struct {
	Heading string
	Words   []string
}

func seededRow() doorRow {
	owner := "acme"
	return doorRow{
		id:       uuid.New(),
		Stamped:  time.Unix(1700000000, 0).UTC(),
		Title:    "keep",
		Labels:   []string{"one", "two"},
		Notes:    map[string]string{"owner": "acme"},
		Owner:    &owner,
		Sections: []doorSection{{Heading: "Body", Words: []string{"first"}}},
	}
}

// TestEveryDoorOfAStoreHandsOutARowThatSharesNoStorageWithTheRowItHolds is `detached` at every
// door: the copy a caller holds is its own, whatever it writes into it, and the tenant's row is
// the row that was seeded — read back through Get, which is the reading the cure keeps.
func TestEveryDoorOfAStoreHandsOutARowThatSharesNoStorageWithTheRowItHolds(t *testing.T) {
	ctx := oneTenantContext()
	store := NewStore(func(r doorRow) uuid.UUID { return r.id })
	seeded := seededRow()
	store.Put(ctx, seeded)

	doorways := []struct {
		name string
		read func(*Store[doorRow]) doorRow
	}{
		{"Get", func(s *Store[doorRow]) doorRow {
			got, err := s.Get(ctx, seeded.id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			return got
		}},
		{"All", func(s *Store[doorRow]) doorRow {
			all := s.All(ctx)
			if len(all) != 1 {
				t.Fatalf("All returned %d rows of the one tenant's store", len(all))
			}
			return all[0]
		}},
		{"Find", func(s *Store[doorRow]) doorRow {
			got, ok := s.Find(ctx, func(r doorRow) bool { return r.id == seeded.id })
			if !ok {
				t.Fatal("Find did not find the row this case seeded")
			}
			return got
		}},
	}

	for _, door := range doorways {
		held := door.read(store)
		// The copy is asked to have kept the row, so a "fix" that hands back a row of its own
		// invention cannot pass by having lost the headers on the way through.
		if held.id != seeded.id || !held.Stamped.Equal(seeded.Stamped) || held.Title != "keep" || held.Nil != nil {
			t.Errorf("%s handed back %v; the copy is a copy of the row, not a row of the copy's own", door.name, held)
		}
		// Every way a command writes a row of this shape, written into what this door handed out.
		held.Labels[0] = "moved"
		held.Notes["owner"] = "moved"
		*held.Owner = "moved"
		held.Sections[0].Words[0] = "moved"
		held.Sections[0].Heading = "moved"

		stored, err := store.Get(ctx, seeded.id)
		if err != nil {
			t.Fatalf("reading the row back after %s: %v", door.name, err)
		}
		for _, field := range []struct{ what, was, is string }{
			{"Labels[0]", "one", stored.Labels[0]},
			{`Notes["owner"]`, "acme", stored.Notes["owner"]},
			{"*Owner", "acme", *stored.Owner},
			{"Sections[0].Words[0]", "first", stored.Sections[0].Words[0]},
			{"Sections[0].Heading", "Body", stored.Sections[0].Heading},
		} {
			if field.is != field.was {
				t.Errorf("%s: the tenant's row reads %s=%q after a caller wrote into the row that door handed it; want %q — that door shares storage with the row it holds, so a command refused after it decided what to write leaves its write behind, and the row is the tenant's",
					door.name, field.what, field.is, field.was)
			}
		}
	}
}
