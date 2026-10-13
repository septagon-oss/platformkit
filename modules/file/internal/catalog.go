package internal

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// catalog declares the routes of handler.go to the typed catalog: the entity they
// are mounted on, the door among them that generic CRUD cannot express, and the
// shape of each. /api/v1/app/resources answers from this registration, and a
// cross-client shell or a generated client reads that answer rather than this
// file. The routes and the declaration are one fact seen from two sides; a module
// that writes its own JSON routes still registers, because doors that only the
// browser they were hand-written for can find are doors nobody else has.
//
// No screen is published. The workspace address a person reads files at is this
// module's own library page, mounted in library_page.go, and a generated screen
// over the same entity would be a second library beside it.
func catalog(surfaces httpx.Surfaces, svc door) {
	surfaces.RegisterResource(httpx.Resource{
		Module: "file",
		Entity: "file",
		Path:   path,
		Read:   contracts.PermissionFileRead,
		Write:  contracts.PermissionFileManage,
		// The three doors a caller that never compiled against this module can
		// perform: the list, the read and the delete. No create is named, because the
		// upload is a stream and a generated form or client has no JSON body to send
		// it with; no update is named, because a file's bytes are what they are, and
		// the only writable fact about one is its caption, which is the command below
		// rather than a PATCH over the row.
		Operations: []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead, httpx.CRUDDelete},
		Schema: crud.Schema{
			Module: "file", Entity: "file",
			Path:   surfaces.App.Prefix() + path,
			Fields: crud.Fields[*contracts.File](),
		},
		Present: entity.EntryHints{
			Singular: "file", Plural: "files", Icon: "file",
			// The row says its name; the facts a person picks a file by are what it
			// is, how big it is and what it says about itself.
			PrimaryField:  "name",
			SummaryFields: []string{"contentType", "size", "alt"},
			Sortable:      []string{"name", "size", "createdAt", "updatedAt"},
		},
	})

	// The caption is not one of the five: it is a claim about a row rather than an
	// edit of its columns, it carries the instant the caller was shown, and it
	// publishes file.altered rather than file.updated. Declared with the work
	// beside it, so a catalog that offers the door is a catalog that can perform it.
	surfaces.AddCommand("file", "file", httpx.Command{
		Verb:        "alt",
		Summary:     "Write what a file says about itself",
		Description: "Alt text, or the claim that the image is decorative and says nothing. A caller that read the row and sends back no updatedAt is refused rather than allowed to overwrite whoever moved it last.",
		Auth:        httpx.Permission(contracts.PermissionFileManage),
		Endpoint:    surfaces.App.Prefix() + path + "/{id}/alt",
		Fields:      crud.FieldsOf(reflect.TypeFor[altBody]()),
		Present:     entity.CommandHints{Label: "Caption", SuccessMessage: "The caption was written."},
		Run:         caption(svc),
	})
}

// caption is the command's work: the same service call the PUT route makes, in
// the request's own transaction, behind the guard registration installed. It
// reads the three declared fields out of the submitted values rather than through
// a decoder of its own, because a value that is not one of the three shapes the
// schema describes is a refusal naming what was expected, and the two other
// places that decode a file request — the JSON body and the form — each have the
// rule for the shape they accept.
func caption(svc door) func(ctx context.Context, id uuid.UUID, values map[string]any) error {
	return func(ctx context.Context, id uuid.UUID, values map[string]any) error {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return problem.New(http.StatusServiceUnavailable, "this command is only performed inside a request")
		}
		alt, err := text(values, "alt")
		if err != nil {
			return err
		}
		decorative, err := flag(values, "decorative")
		if err != nil {
			return err
		}
		expected, err := instant(values, "expectedUpdatedAt")
		if err != nil {
			return err
		}
		_, err = svc.SetAlt(ctx, tx, id, alt, decorative, expected)
		return err
	}
}

func text(values map[string]any, field string) (string, error) {
	v, ok := values[field]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", problem.New(http.StatusUnprocessableEntity, field+" is text")
	}
	return s, nil
}

func flag(values map[string]any, field string) (bool, error) {
	v, ok := values[field]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, problem.New(http.StatusUnprocessableEntity, field+" is true or false")
	}
	return b, nil
}

func instant(values map[string]any, field string) (time.Time, error) {
	v, ok := values[field]
	if !ok || v == nil {
		return time.Time{}, nil
	}
	switch instant := v.(type) {
	case time.Time:
		return instant.UTC(), nil
	case string:
		stamped, err := time.Parse(time.RFC3339, instant)
		if err != nil {
			return time.Time{}, problem.New(http.StatusUnprocessableEntity,
				field+" is an instant in the form 2026-01-01T00:00:00Z")
		}
		return stamped.UTC(), nil
	}
	return time.Time{}, problem.New(http.StatusUnprocessableEntity, field+" is an instant")
}
