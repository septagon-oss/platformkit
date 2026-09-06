package contracts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func draft() contracts.Content {
	c := contracts.Content{Slug: "about-us", Title: "About us", Kind: contracts.KindPage, Status: contracts.StatusDraft}
	c.ID = uuid.New()
	return c
}

// TestTheLifecycleDecisions is the rules with no database under them: what each
// command answers from each state, and that the answer is a function of its
// arguments alone.
func TestTheLifecycleDecisions(t *testing.T) {
	live, err := contracts.Publish(draft(), at)
	if err != nil || live.Event != contracts.EventPublished || live.Next.Status != contracts.StatusPublished || !live.Next.PublishedAt.Equal(at) {
		t.Fatalf("publishing a draft = %+v, %v; want it published at %v and announced", live, err, at)
	}
	if p, ok := live.Payload.(contracts.Moved); !ok || p.ContentID != live.Next.ID || p.Status != contracts.StatusPublished || !p.At.Equal(at) {
		t.Errorf("the payload is %+v; want the content, its status and the instant", live.Payload)
	}

	again, err := contracts.Publish(live.Next, at.Add(time.Hour))
	if err != nil || again.Event != "" || !again.Next.PublishedAt.Equal(at) {
		t.Errorf("publishing published content = %+v, %v; want silence and the time left where it was", again, err)
	}

	filed := contracts.Archive(live.Next, at)
	if filed.Event != contracts.EventArchived || filed.Next.Status != contracts.StatusArchived || filed.Next.PublishedAt != nil {
		t.Errorf("archiving = %+v; want it archived, not published, and announced", filed)
	}
	if _, err := contracts.Publish(filed.Next, at); !errors.Is(err, crud.ErrConflict) {
		t.Errorf("publishing from the archive = %v; want a conflict", err)
	}

	back := contracts.Unpublish(filed.Next, at)
	if back.Event != contracts.EventUnpublished || back.Next.Status != contracts.StatusDraft || back.Next.PublishedAt != nil {
		t.Errorf("unpublishing from the archive = %+v; want a draft, announced", back)
	}

	// The caller's copy is not touched: a decision answers, it does not act.
	before := draft()
	if _, err := contracts.Publish(before, at); err != nil || before.Status != contracts.StatusDraft || before.PublishedAt != nil {
		t.Errorf("Publish changed its argument to %+v; a decision is a function of its inputs", before)
	}
}
