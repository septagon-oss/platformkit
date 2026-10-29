package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// transaction is the request's, or a 503 saying why there is none. A handler
// that cannot reach the database has nothing to answer with, and the middleware
// has already logged the cause.
func transaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return tx, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, nil
}

// Fault turns the three errors a caller can act on into the response that says
// so: 404 for a row this tenant does not have, 422 for something the caller
// sent, 409 for a state the write contradicts. Anything else is an outage and
// reaches huma as a 500 with its cause in the log and nothing in the body.
//
// It is exported because a module's own handlers answer with the same three
// errors as the five routes here, and one mapping is the point: a module that
// wrote its own would be a second opinion about what a 404 means.
func Fault(err error) error {
	if fieldErr, ok := errors.AsType[*richTextFieldError](err); ok {
		p := &richTextProblem{
			Problem: problem.New(http.StatusUnprocessableEntity, fieldErr.Error()),
			field:   fieldErr.field, issues: fieldErr.refused.Issues,
		}
		for _, issue := range fieldErr.refused.Issues {
			p.Errors = append(p.Errors, fmt.Sprintf("%s: line %d: %s: %s", fieldErr.field, issue.Line, issue.Construct, issue.Remedy))
		}
		return p
	}
	switch {
	case errors.Is(err, tenancy.ErrPolicyUnavailable), errors.Is(err, tenancy.ErrInvalidPolicyRequest):
		return problem.New(http.StatusServiceUnavailable, "POLICY_UNAVAILABLE: authorization is temporarily unavailable")
	case errors.Is(err, tenancy.ErrPolicyDenied):
		return problem.New(http.StatusForbidden, "POLICY_DENIED: this action is not allowed")
	case errors.Is(err, crud.ErrNotFound):
		return problem.NotFound("no such row, or none this tenant may see")
	case errors.Is(err, crud.ErrInvalid):
		violations, _ := errors.AsType[fault.Violations](err)
		return problem.Refusal(err.Error(), violations, err)
	case errors.Is(err, crud.ErrConflict):
		if _, unique := errors.AsType[*crud.UniqueConflict](err); unique {
			return problem.Conflict("A record already uses one of these values. Change the duplicate value and try again.")
		}
		return problem.Conflict(err.Error())
	case errors.Is(err, ErrStaleTag):
		// Precondition Failed, the status conditional writes answer with, and the row's
		// current number in the sentence, because the cure is one re-read.
		return problem.New(http.StatusPreconditionFailed, err.Error())
	default:
		return err
	}
}

// idInput is the item routes' path parameter.
type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
}

// bodyInput is the create route's body: the entity itself, minus whatever it
// says about the fields the server owns.
//
// required, because T is a pointer type and huma reads a pointer body as
// optional: a POST with no body at all used to arrive here as a nil entity and
// panic on the first field the handler stamped. The tag makes it a 400 that
// says what is missing; crud.Create's own nil check is the second half, because
// this package is also called from a module's own handlers.
type bodyInput[T any] struct {
	Body T `required:"true"`
	// RawBody is the same bytes, which huma fills from the same read. It is
	// here for one question the decoded entity cannot answer: did the caller
	// name this field at all. See refuseImmutable.
	RawBody []byte
}

type patchInput struct {
	ID   uuid.UUID      `path:"id" format:"uuid" doc:"The row's id"`
	Body map[string]any `doc:"The writable fields to change"`
	// IfMatch is the ETag the caller read, if it quoted one. Absent means "write
	// whatever is there", which stays allowed: the tag is how a client that read the row
	// says so, and every client that never read one — the documents this repository
	// checked in, the mobile flows, every caller written before this door — keeps
	// working. A tag the row has passed is a 412 naming the number it is on now, and a
	// resource whose entity carries no write count has no tag to read and answers 422.
	IfMatch string `header:"If-Match" doc:"The row's ETag, as read; a tag the row has passed refuses the write with 412. A resource whose entity carries no write count answers 422."`
}

// deleteInput is the delete route's parameters: which row, and the same conditional
// quote the patch honours. Removing a row somebody else has just written is the
// largest version of the defect a tag exists to close.
type deleteInput struct {
	ID      uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
	IfMatch string    `header:"If-Match" doc:"The row's ETag, as read; a tag the row has passed refuses the delete with 412. A resource whose entity carries no write count has no tag to read and answers 422."`
}

// taggedItem is one entity answered with the number of writes the row has had, which
// is the same number quoted back as If-Match to write against it.
//
// It is this package's and not Item's, because the tag exists only where the kernel
// counts writes: an entity with no revision has no tag to send, and a documented but
// always-empty header on every module's responses would be a field nothing reads.
type taggedItem[T any] struct {
	ETag string `header:"ETag" doc:"This row's own write count, as quoted by If-Match"`
	Body T
}

// Item is one entity as a response body, and Page is a page of them.
//
// They are exported because a module that writes its own handlers answers in
// the same shape as the five routes here — modules/audit and
// modules/notification both do, an append-only trail and a per-recipient list
// being things a Spec is not — and three separately declared response shapes
// are three spellings of the same JSON for a client to discover the hard way.
type Item[T any] struct {
	Body T
}

// Page is a page of rows and the total it came from. The limit and the offset
// are echoed, so a caller that sent neither knows what it got.
type Page[T any] struct {
	Body struct {
		Items  []T   `json:"items"`
		Total  int64 `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
}

// listInput is the page, the order and the filters, as query parameters.
// Filters repeat — ?filter=status:open&filter=priority:2 — rather than sharing
// one comma-separated value, because a value may contain a comma and a field
// name may not contain a colon.
type listInput struct {
	Limit  int      `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int      `query:"offset" minimum:"0" doc:"Rows to skip"`
	Sort   string   `query:"sort" doc:"A field name, or a field name prefixed with - for descending"`
	Filter []string `query:"filter" doc:"field:value, repeated"`
}

func (in *listInput) query(fields []crud.Field) (crud.Query, error) {
	q := crud.Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort}
	if len(in.Filter) == 0 {
		return q, nil
	}
	q.Filter = make(map[string]any, len(in.Filter))
	for _, raw := range in.Filter {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return crud.Query{}, fmt.Errorf("%w: filter %q is not \"field:value\"", crud.ErrInvalid, raw)
		}
		f, known := crud.FieldNamed(fields, name)
		if !known {
			return crud.Query{}, fmt.Errorf("%w: there is no field %q to filter on", crud.ErrInvalid, name)
		}
		typed, err := coerce(f, value)
		if err != nil {
			return crud.Query{}, err
		}
		q.Filter[name] = typed
	}
	return q, nil
}

// coerce reads a query parameter as the field's own type, so a filter on an
// integer column compares integers. Postgres would raise on the mismatch, which
// is a 500 for what is really a malformed request.
// coerce reads a filter value as the field's own type. The table is kit/crud's,
// because a declared highlight value and a ?filter= value are the same question —
// what is this field's name worth as this field's type — and two tables answering
// it would be two opinions about which filters do nothing.
func coerce(f crud.Field, raw string) (any, error) { return crud.Coerce(f, raw) }
