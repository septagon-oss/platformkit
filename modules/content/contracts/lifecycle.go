package contracts

import (
	"fmt"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// The lifecycle's three decisions. Each takes the content as it is and the
// instant the command runs, and answers with what the content is next and the
// event that announces it; none touches a database, a clock or the caller's
// copy, so the same content at the same instant always answers the same way.
// internal applies them to a row and contenttest's fake applies them to a map,
// which is how the two agree by construction rather than by a mirrored copy.

// Publish serves it to anybody, and records when. Publishing what is already
// published changes nothing — a second click on the button is not a second
// publication, and the time does not move. Archived content is refused: it is
// unpublished first, which is what takes it out of the archive.
func Publish(c Content, at time.Time) (crud.Outcome[Content], error) {
	switch c.Status {
	case StatusPublished:
		return crud.Outcome[Content]{Next: c}, nil
	case StatusArchived:
		return crud.Outcome[Content]{}, fmt.Errorf("%w: archived content is not published from the archive; unpublish it first", crud.ErrConflict)
	}
	c.Status, c.PublishedAt = StatusPublished, &at
	return moved(c, EventPublished, at), nil
}

// Unpublish takes it back to a draft, from published or from archived, and
// clears the publication time. It cannot refuse.
func Unpublish(c Content, at time.Time) crud.Outcome[Content] {
	return to(c, StatusDraft, EventUnpublished, at)
}

// Archive keeps it and serves it to nobody. It cannot refuse.
func Archive(c Content, at time.Time) crud.Outcome[Content] {
	return to(c, StatusArchived, EventArchived, at)
}

// to moves content to a status that is not published, from any other. Both
// clear the publication time, because "published means published at a time" is
// one fact and this is the half that puts it away. Content already there stays
// as it is, silently.
func to(c Content, status, event string, at time.Time) crud.Outcome[Content] {
	if c.Status == status {
		return crud.Outcome[Content]{Next: c}
	}
	c.Status, c.PublishedAt = status, nil
	return moved(c, event, at)
}

// moved is a change and the event that announces it.
func moved(c Content, event string, at time.Time) crud.Outcome[Content] {
	return crud.Outcome[Content]{Next: c, Event: event, Payload: Moved{
		ContentID: c.ID, Slug: c.Slug, Kind: c.Kind, Status: c.Status, At: at,
	}}
}
