package notificationtest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func markReadOnce(t *testing.T, f Fixture) {
	got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	read, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if read.ReadAt == nil {
		t.Error("a notification marked read has no time on it")
	}
	f.silent(t, "MarkRead again", func() {
		if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada); err != nil {
			t.Fatalf("MarkRead again: %v", err)
		}
	})
	published(t, f, contracts.EventCreated, contracts.EventRead)
}

func concealAnotherRecipientsNotice(t *testing.T, f Fixture) {
	got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	// Not a 403: the only thing a caller may learn about a
	// notification that is not theirs is that they do not have one
	// with that id.
	if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Bob); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("MarkRead of somebody else's notification = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.MarkRead(f.Ctx, f.Tx, uuid.New(), Ada); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("MarkRead of an unknown id = %v, want ErrNotFound", err)
	}
}

func preserveListRecipient(t *testing.T, f Fixture) {
	// The recipient is the caller, resolved from the principal, and it
	// is not a parameter. Both implementations set the filter rather
	// than merging it, so there is no shape of Query that lists
	// somebody else's rows — which is what lets the route be SignedIn
	// with no permission at all. An implementation that honoured the
	// caller's filter would turn one signed-in person into a reader of
	// everybody's notifications, and it would look like paging.
	if _, err := f.Service.Notify(f.Ctx, f.Tx, notice("Ada's")); err != nil {
		t.Fatalf("Notify Ada: %v", err)
	}
	hers := notice("Bob's")
	hers.Recipient = Bob
	if _, err := f.Service.Notify(f.Ctx, f.Tx, hers); err != nil {
		t.Fatalf("Notify Bob: %v", err)
	}
	rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Bob, crud.Query{
		Filter: map[string]any{"recipientId": Ada},
	})
	if err != nil {
		t.Fatalf("ListFor with a filter of the caller's own: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].RecipientID != Bob {
		t.Fatalf("asking for Ada's rows as Bob returned %d of %d; the list is the caller's", len(rows), total)
	}
}

func listNewestFirst(t *testing.T, f Fixture) {
	for _, title := range []string{"first", "second"} {
		if _, err := f.Service.Notify(f.Ctx, f.Tx, notice(title)); err != nil {
			t.Fatalf("Notify %s: %v", title, err)
		}
	}
	other := notice("not yours")
	other.Recipient = Bob
	if _, err := f.Service.Notify(f.Ctx, f.Tx, other); err != nil {
		t.Fatalf("Notify Bob: %v", err)
	}

	rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{})
	if err != nil {
		t.Fatalf("ListFor: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("Ada's list holds %d of %d rows, want her own two", len(rows), total)
	}
	if rows[0].Title != "second" {
		t.Errorf("the list starts with %q, want the newest first", rows[0].Title)
	}
	for _, row := range rows {
		if row.RecipientID != Ada {
			t.Errorf("Ada's list holds a row addressed to %s", row.RecipientID)
		}
	}
	if rows, _, err = f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{Limit: 1, Offset: 1}); err != nil ||
		len(rows) != 1 || rows[0].Title != "first" {
		t.Errorf("the second page of Ada's list = %v, %v", rows, err)
	}
}
