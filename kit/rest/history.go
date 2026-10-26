package rest

// history.go is the version a mounted resource keeps of every write it commits.
//
// A version is written by the door that wrote the row, in the same transaction,
// after the UPDATE that normalised the values and before the event that announces
// them: before the write it would record what a caller asked for rather than what the
// row holds (crud.Update runs the entity's own Validate), and after the commit it
// would leave an event whose version does not exist. The version number is the row's
// own write count (entity.RevisionField), read after the UPDATE that moved it; mount
// refuses a History Spec whose entity carries no write count.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// RestoredEvent and Restored are the write this door publishes, spelled where the
// contract lives: kit/httpx, beside the Versions port a version is kept behind,
// so that the module which declares the event typed can name it without importing
// this package. These two lines name the same contract for the callers that reach
// it through the door that publishes it; they are not a second spelling of it.
const RestoredEvent = httpx.RestoredEvent

// Restored is RestoredEvent's payload. See kit/httpx for what each name means and
// why the pair of From and To is the answer a person reads after a restore.
type Restored = httpx.Restored

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

// restoreReasonPrefix opens the one write reason this door spells out rather than
// handing through: a version written back rather than written forward.
const restoreReasonPrefix = "restore:"

// RestoreWriteReason is the reason a version row files when the write brought
// version from back: "restore:3" for the third version. It is exported because the
// panel beside the record reads that reason back, and the two must spell it alike —
// the reason is stored text, so its spelling belongs to the door that writes it and
// not to whichever screen draws it.
func RestoreWriteReason(from int64) string {
	return restoreReasonPrefix + strconv.FormatInt(from, 10)
}

// RestoredVersion answers the version a stored write reason names, and false for
// every other reason — an ordinary save's empty one included. A reason that names no
// number is treated as no reason: the row keeps whatever it says, and the panel says
// nothing about where the write came from.
func RestoredVersion(reason string) (int64, bool) {
	if !strings.HasPrefix(reason, restoreReasonPrefix) {
		return 0, false
	}
	from, err := strconv.ParseInt(strings.TrimPrefix(reason, restoreReasonPrefix), 10, 64)
	if err != nil || from < 1 {
		return 0, false
	}
	return from, true
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
		At:    entity.BaseOf(e).UpdatedAt,
		Actor: actorOf(ctx),
		Refs:  bodyReferences(fields, e),
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

// restoreWarning is the consequence restoring has, in the words its author
// declared. One value because two people have to say it identically: the generated
// form for the command, and the panel beside the record whose version is being
// brought back. A screen that reworded it would be a second copy of what a write
// costs.
var restoreWarning = entity.CommandConfirmation{
	Title:        "Restore this version?",
	Body:         "A new version will be written with this content. Nothing is deleted.",
	ConfirmLabel: "Restore",
}

// mountHistory puts the two routes a History resource has beyond the five: the
// read of one record's series, and the restore. Both are mounted by this Spec and
// not by the store's module, because the guard, the surface, the schema and the
// record's address all belong to the record — a module mounting a page beside the
// detail view it describes would be a second owner of that record's screens.
func (s Spec[T]) mountHistory(surfaces httpx.Surfaces, read *httpx.Router) {
	if s.offers(httpx.CRUDRead) {
		httpx.Register(read, s.op("versions", http.MethodGet, s.item()+"/versions", 0,
			"List this "+s.Entity+"'s versions", "Newest first, one locale at a time. A version is one committed write."),
			s.readAuth(), func(ctx context.Context, in *versionsInput) (*versionsOutput, error) {
				tx, err := transaction(ctx)
				if err != nil {
					return nil, err
				}
				if _, err := crud.GetCtx[T](ctx, tx, in.ID); err != nil {
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

	// A version is the one thing a restore cannot be asked for without; the other
	// two are conditions with documented defaults, and a body that leaves them out
	// means the default rather than being refused for not saying it.
	type restoreArgument struct {
		Version int64 `json:"version" doc:"The version to write back, as the list answered it"`
		// Locale is omitempty in both senses: the series a restore reads from is the
		// record's own language when the body names none, and a form drawn for the
		// record cannot be expected to name a tag it was never shown.
		Locale string `json:"locale,omitempty" doc:"The series to read it from; the record's own language when unset"`
		// Expected carries no condition when the caller read no revision; the door
		// then writes as an ordinary PATCH would.
		Expected int64 `json:"expectedRevision,omitempty" doc:"The row's write count as the caller read it; a row past it is refused with 412 and writes nothing"`
	}
	Command(surfaces, s, restoreVerb, "Restore a version of this "+s.Entity,
		"Writes a new version with the stored one's content. No version is changed or removed, and the row's lifecycle fields are not replayed.",
		[]string{RestoredEvent},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in restoreArgument) (T, error) {
			e, err := s.restoreVersion(ctx, tx, id, in.Version, s.versionLocale(ctx, in.Locale), in.Expected)
			if err != nil {
				return e, Fault(err)
			}
			return e, nil
		}, CommandOptions{Present: entity.CommandHints{
			Label:          "Restore this version",
			Confirmation:   &restoreWarning,
			SuccessMessage: "A new version was written with the content you restored.",
		}})
}

// restoreVerb is the last path segment of the command that writes a stored
// version back, on the API and on the screen alike. One name for both because the
// screen performs this command and not a lookalike of it.
const restoreVerb = "restore"

// historyPort is this Spec's version series as a screen reads it: the verb of the
// door above, and the series itself, read a page at a time in the request's own
// transaction and filed under the language this request is served in — the same
// answer the versions route gives, so the panel and the API cannot disagree about
// which series a record has.
func (s Spec[T]) historyPort() *httpx.History {
	if !s.History {
		return nil
	}
	return &httpx.History{RestoreVerb: restoreVerb, Asks: restoreWarning.Body,
		Versions: func(ctx context.Context, id uuid.UUID) ([]httpx.Version, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			return s.Versions.ListVersions(ctx, tx, httpx.VersionQuery{
				Module: s.Module, Entity: s.Entity, Locale: s.versionLocale(ctx, ""),
				Record: id, Limit: versionsLimit,
			})
		}}
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
		return zero, problem.New(http.StatusUnprocessableEntity,
			httpx.CodeVersionUnusable+": a version is numbered from 1, and "+strconv.FormatInt(from, 10)+" is not one")
	}
	stored, err := s.Versions.Version(ctx, tx, httpx.VersionKey{
		Module: s.Module, Entity: s.Entity, Locale: locale, Record: id, Version: from,
	})
	if err != nil {
		return zero, err
	}
	fields := crud.Fields[T]()
	values, err := replayable(fields, s.Immutable, stored.Fields, s.freshRow())
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
	e, err := s.updateRow(WithWriteReason(ctx, RestoreWriteReason(from)), tx, id, fields, values, tag)
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
// names the schema has a writable column for, and nothing else. It is one function so
// that the kit's restore and anything else replaying a version answer alike.
//
// Two encodings are held to here. A key the stored document does not carry is replayed
// as the field's own empty value: a version is the whole writable document, so a body
// that was empty when it was written has no key, and leaving that key out would make
// the restore a partial write keeping whatever the row has since become. And a number
// travels as the bytes the version holds, never as a decoded float: a minor unit is an
// int64 and JSON's only number is a float64, which rounds the last digits of the money.
func replayable(fields []crud.Field, immutable []string, document json.RawMessage, fresh any) (map[string]any, error) {
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(document, &stored); err != nil {
		return nil, fmt.Errorf("rest: that version's document cannot be read back: %w", err)
	}
	blank := reflect.ValueOf(fresh).Elem()
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		if entity.ServerOwned(field) || foldedName(map[string]any{field.Name: nil}, immutable) != "" {
			continue
		}
		if raw, ok := stored[field.Name]; ok {
			out[field.Name] = raw
			continue
		}
		empty, err := json.Marshal(blank.FieldByIndex(field.Index).Interface())
		if err != nil {
			return nil, fmt.Errorf("rest: that version's %s cannot be written back empty: %w", field.Name, err)
		}
		out[field.Name] = json.RawMessage(empty)
	}
	return out, nil
}

// freshRow is an untouchable copy of this Spec's entity, so that replay can ask
// what one of its fields holds when nothing was ever written to it.
func (s Spec[T]) freshRow() any {
	return reflect.New(reflect.TypeFor[T]().Elem()).Interface()
}
