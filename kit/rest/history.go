package rest

// history.go is the version a mounted resource keeps of every write it commits.
//
// One decision runs through this file: a version is written by the door that
// wrote the row, in the same transaction, after the UPDATE that normalised the
// values and before the event that announces them. Recording before the write
// would store what a caller asked for rather than what the row holds — crud.Update
// runs the entity's own Validate, which trims and defaults — and recording after
// the commit would leave a window where the event exists and the version it
// describes does not, which is the half-written state rule 9 refuses. A resource
// that records nothing is a resource whose users cannot see what changed; a
// resource that records in a subscriber is a resource that records late.
//
// The version number is the row's own write count (entity.RevisionField), read
// after the UPDATE that moved it. There is no counter here, and mount refuses a
// History Spec whose entity carries no write count, because a version nobody can
// quote against a row is a number with no meaning.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// RestoredEvent names the write a restore performed. It is spelled here because
// the kit publishes it, for whichever resource asked for history, in the same way
// it publishes the three CRUD events of the resources it mounts; the module that
// owns the versions table declares it typed in its manifest, which is what makes
// a composition that mounts a History resource without one a boot refusal.
const RestoredEvent = "history.restored"

// Restored is RestoredEvent's payload: which record, which version came back,
// which number the write landed on, in which language, and when. From and To are
// the two numbers a person reads after a restore — "Restored from v3" and the
// version the record is on now — and the pair is why the payload is not simply
// the entity: the entity says what the row is, and only this says a version of it
// was brought back.
type Restored struct {
	Module, Entity string
	Record         uuid.UUID
	From           int64
	To             int64
	Locale         string
	Actor          uuid.UUID
	At             time.Time
}

// writeReasonKey carries one write's own sentence about why it happened down to
// the version row it produces. A reason belongs here and not on a parameter of
// UpdateRow because the door that records is three calls below the door a caller
// uses, and a parameter would either be threaded through every exported write or
// be missing where it mattered. An ordinary save carries none.
type writeReasonKey struct{}

// WithWriteReason returns ctx carrying the reason a version row records for the
// write performed in it — "restore:3" for a restore, empty for everybody else.
// It is empty-string-removes semantics: a reason of "" takes any reason out.
func WithWriteReason(ctx context.Context, reason string) context.Context {
	if reason == "" {
		return context.WithValue(ctx, writeReasonKey{}, "")
	}
	return context.WithValue(ctx, writeReasonKey{}, reason)
}

func writeReason(ctx context.Context) string {
	r, _ := ctx.Value(writeReasonKey{}).(string)
	return r
}

// recordVersion writes the version this committed write produced. snap is the row
// as it stands after the UPDATE — the values the database holds, with the diff
// still off it — and changed names the fields this write moved, empty when
// nothing preceded it.
//
// The store files the images a stored body shows as that version's uses, from the
// document written here: a retained version holds its pictures for as long as it
// is retained and no longer, and the store is what knows how long that is.
func (s Spec[T]) recordVersion(ctx context.Context, tx db.Tx[db.Tenant], e T, snap map[string]json.RawMessage, changed []string) error {
	if !s.History {
		return nil
	}
	document, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("rest: this write's version cannot be written down: %w", err)
	}
	fields := crud.Fields[T]()
	_, err = s.Versions.RecordVersion(ctx, tx, httpx.VersionInput{
		Module: s.Module, Entity: s.Entity, Locale: recordLocale(ctx),
		Record: entity.BaseOf(e).ID, Version: s.revisionOf(fields, e),
		Fields: document, Changed: changed, Reason: writeReason(ctx),
		At:   entity.BaseOf(e).UpdatedAt,
		Refs: bodyReferences(fields, e),
	})
	return err
}

// historyFault refuses a History declaration that could not keep a version. A
// silently dropped version is a document's content nobody can get back, which is
// the loss rule 8 refuses, so all three answers are boot refusals naming the
// declaration to fix rather than a log line beside a quiet default. The second is
// why turning history on for a resource means the row counts its own writes: the
// version number is that count, and nothing here invents a second one.
func (s Spec[T]) historyFault() string {
	if !s.History {
		return ""
	}
	switch {
	case s.Versions == nil:
		return fmt.Sprintf("keeps versions of %s.%s and names no store to keep them in", s.Module, s.Entity)
	case revisionColumn(crud.Fields[T]()) == "":
		return fmt.Sprintf("numbers its %s.%s versions with the row's own write count, and the entity carries no %q field",
			s.Module, s.Entity, RevisionField)
	case !s.offersWrite():
		return fmt.Sprintf("keeps versions of %s.%s and mounts no write to record", s.Module, s.Entity)
	}
	return ""
}

// actorOf is who made this write, in the one spelling a version stores: nobody
// signed in, and no system identity was minted to stand in for them, so the row
// answers NULL rather than an id that would read back as a person.
func actorOf(ctx context.Context) uuid.UUID {
	actor, _ := tenancy.ActorFrom(ctx)
	return actor
}

// bodyReferences reads the files each richtext field of this row shows, taken off
// the parsed document and not off a scan of the string: what a version keeps alive
// is what its body would actually render, and a reference the renderer would drop
// keeps a file alive for a picture nobody ever saw. This is the same reading
// kit/rest's own use-recording does, for the same reason.
func bodyReferences(fields []crud.Field, e any) []httpx.VersionRefs {
	var out []httpx.VersionRefs
	value := reflect.ValueOf(e).Elem()
	for _, field := range fields {
		if field.Widget != "richtext" {
			continue
		}
		d, err := richtext.Parse(value.FieldByIndex(field.Index).String())
		if err != nil {
			continue
		}
		refs := richtext.References(d)
		if len(refs) == 0 {
			continue
		}
		ids := make([]uuid.UUID, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}
		out = append(out, httpx.VersionRefs{Field: field.Column, Files: ids})
	}
	return out
}

// versionPage is what GET {item}/versions answers: this tenant's versions of one
// record, newest first, and the version an earlier page starts below.
type versionPage struct {
	Items []httpx.Version `json:"items"`
	// Next, above zero, is the version to ask the next page for; zero says this
	// page reached the beginning of the series.
	Next int64 `json:"next"`
}

type versionsOutput struct{ Body versionPage }

type versionsInput struct {
	ID     uuid.UUID `path:"id" format:"uuid" doc:"The record whose versions are read"`
	Locale string    `query:"locale" doc:"The version series, by BCP-47 tag; the record's own language when unset"`
	Before int64     `query:"before" doc:"Ask for the versions older than this one"`
	Limit  int       `query:"limit" doc:"How many versions to answer with, at most"`
}

// versionsLimit is the page a versions read answers with when the caller asked
// for none, and its ceiling: the panel shows a page and offers an earlier one, and
// a resource that could be asked for a record's whole history in one response is a
// resource whose largest record is read out of the database on every page view.
const versionsLimit = 20

// mountHistory puts the two routes a History resource has beyond the five: the
// read of one record's series, and the restore. Both are mounted by this Spec and
// not by the store's module, because the guard, the surface, the schema and the
// record's address all belong to the record — a module mounting a page beside the
// detail view it describes would be a second owner of that record's screens.
func (s Spec[T]) mountHistory(surfaces httpx.Surfaces, read, write *httpx.Router) {
	if s.offers(httpx.CRUDRead) {
		httpx.Register(read, s.op("versions", http.MethodGet, s.item()+"/versions", 0,
			"List this "+s.Entity+"'s versions", "Newest first, one locale at a time. A version is one committed write."),
			s.readAuth(), func(ctx context.Context, in *versionsInput) (*versionsOutput, error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				if _, err := crud.Get[T](tx, in.ID); err != nil {
					return nil, Fault(err)
				}
				limit := in.Limit
				if limit <= 0 || limit > versionsLimit {
					limit = versionsLimit
				}
				items, err := s.Versions.ListVersions(ctx, tx, httpx.VersionQuery{
					Module: s.Module, Entity: s.Entity, Locale: s.versionLocale(ctx, in.Locale),
					Record: in.ID, Limit: limit + 1, Before: in.Before,
				})
				if err != nil {
					return nil, Fault(err)
				}
				out := &versionsOutput{}
				out.Body.Items = items
				if len(items) > limit {
					out.Body.Items = items[:limit]
					out.Body.Next = items[limit-1].Version
				}
				return out, nil
			})
	}

	type restoreArgument struct {
		Version  int64  `json:"version" doc:"The version to write back, as the list answered it"`
		Locale   string `json:"locale" doc:"The series to read it from; the record's own language when unset"`
		Expected int64  `json:"expectedRevision" doc:"The row's write count as the caller read it; a row past it is refused with 412 and writes nothing"`
	}
	Command(surfaces, s, "restore", "Restore a version of this "+s.Entity,
		"Writes a new version with the stored one's content. No version is changed or removed, and the row's lifecycle fields are not replayed.",
		[]string{RestoredEvent},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in restoreArgument) (T, error) {
			e, err := s.restoreVersion(ctx, tx, id, in.Version, s.versionLocale(ctx, in.Locale), in.Expected)
			if err != nil {
				return e, Fault(err)
			}
			return e, nil
		}, CommandOptions{Present: entity.CommandHints{
			Label: "Restore this version",
			Confirmation: &entity.CommandConfirmation{
				Title:        "Restore this version?",
				Body:         "A new version will be written with this content. Nothing is deleted.",
				ConfirmLabel: "Restore",
			},
			SuccessMessage: "A new version was written with the content you restored.",
		}})
}

// versionLocale resolves the series a request asked for: its own tag when it sent
// one, and the language this tenant serves otherwise — the same answer every use
// row is filed under, so a version and a use never disagree about which language
// a record's content is in.
func (s Spec[T]) versionLocale(ctx context.Context, asked string) string {
	if asked != "" {
		return asked
	}
	return recordLocale(ctx)
}

// restoreVersion reads one stored version and writes it back as a new one.
//
// The fields replayed are the writable ones only. A stored document is the whole
// row, and the row's lifecycle (its status, its publish time, its author), its
// identity and its write count belong to the doors that own them: a restore that
// replayed them would unpublish a page or forge a byline, which is 0069 §6's
// boundary between history and publish. What is left is what a person means by
// "put that back".
//
// The expected revision is re-checked by the same door a PATCH goes through,
// inside this transaction and under the row lock, so a restore that races an
// edit settles as a 412 that writes nothing and emits nothing.
func (s Spec[T]) restoreVersion(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from int64, locale string, expected int64) (T, error) {
	var zero T
	if from < 1 {
		return zero, fmt.Errorf("%w: a version is numbered from 1", crud.ErrInvalid)
	}
	stored, err := s.Versions.Version(ctx, tx, httpx.VersionKey{
		Module: s.Module, Entity: s.Entity, Locale: locale, Record: id, Version: from,
	})
	if err != nil {
		return zero, err
	}
	fields := crud.Fields[T]()
	values, err := replayable(fields, s.Immutable, stored.Fields)
	if err != nil {
		return zero, err
	}
	if len(values) == 0 {
		return zero, fmt.Errorf("%w: that version holds nothing a write may set", crud.ErrInvalid)
	}
	tag := ""
	if expected > 0 {
		tag = strconv.Quote(strconv.FormatInt(expected, 10))
	}
	e, err := s.updateRow(WithWriteReason(ctx, "restore:"+strconv.FormatInt(from, 10)), tx, id, fields, values, tag)
	if err != nil {
		return e, err
	}
	actor, _ := tenancy.ActorFrom(ctx)
	if err := events.Publish(ctx, tx, RestoredEvent, Restored{
		Module: s.Module, Entity: s.Entity, Record: id, From: from,
		To: s.revisionOf(fields, e), Locale: locale, Actor: actor, At: entity.BaseOf(e).UpdatedAt,
	}); err != nil {
		return e, err
	}
	return e, nil
}

// replayable turns a stored document into the body a write accepts: JSON field
// names the schema has a writable column for, and nothing else. It is one
// function because the answer must not differ between the kit's restore and
// anything else that replays a version.
func replayable(fields []crud.Field, immutable []string, document json.RawMessage) (map[string]any, error) {
	var stored map[string]any
	if err := json.Unmarshal(document, &stored); err != nil {
		return nil, fmt.Errorf("rest: that version's document cannot be read back: %w", err)
	}
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		if entity.ServerOwned(field) || foldedName(map[string]any{field.Name: nil}, immutable) != "" {
			continue
		}
		if value, ok := stored[field.Name]; ok {
			out[field.Name] = value
		}
	}
	return out, nil
}
