// Package saved draws the one page a person returns to for the copies they kept:
// the grid of them, the removal that gives one back, and the save a record screen's
// control posts.
//
// The page is the foundation's and not a module's because it lists one person's
// copies of whatever any composed module declared savable: no single module owns the
// list of everybody's. What it reads is a port the composition implements over the
// saving service it wired — nothing under ui/ may name a module, so the party that
// has met both hands the answer over rather than a renderer guessing at it.
//
// A retained copy may name a record that has since gone, and the grid draws such a
// row rather than hiding it: the person who kept it is the only party who can still
// see it, and giving it back is theirs to do.
package saved

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/page"
)

// relList is where the grid answers, relative to the surface it is mounted on;
// the two commands live under it. They are relative because a surface decides where
// its documents stand, and a page that wrote its own absolute address would be a
// page that cannot be mounted anywhere else.
const relList = "/saved"

// At is where this package's three routes answer on one surface. The composition
// hands the first two to a record screen as its control's targets — a button whose
// form posts to an address nothing answers at is a lie with a button on it — and the
// grid uses the same three to draw and to redirect, so no address is spelled twice.
type At struct {
	List string
	Save string
	// Remove is the prefix one copy's give-back is named after: Remove + id + "/remove".
	Remove string
}

// mounts is the At of one surface: where the three routes will answer once Mount
// mounts them.
func mounts(r *httpx.Router) At {
	return At{
		List:   r.PagePath(relList),
		Save:   r.PagePath(relList + "/save"),
		Remove: r.PagePath(relList),
	}
}

// RemoveAt is the address one kept copy is given back at.
func (a At) RemoveAt(id string) string { return a.Remove + "/" + id + "/remove" }

// Filtered is the grid showing one pair's copies, which is what a record's control
// links to: the list of the thing it is a copy of, and not of everything.
func (a At) Filtered(module, kind string) string {
	if module == "" || kind == "" {
		return a.List
	}
	return a.List + "?module=" + module + "&kind=" + kind
}

// The three states a source may be observed in beside a copy. They are an
// observation of the source and never a state of the copy, which keeps no field of
// its own about whether the record behind it still stands.
const (
	AvailabilityAvailable   = "available"
	AvailabilityUnavailable = "unavailable"
	AvailabilityTemporary   = "temporary"
)

// defaultPerPage is the grid's page when the composition names none: the number the
// saving service itself defaults to, so a page of the grid and a page of the
// service are the same page.
const defaultPerPage = 24

// Row is one kept copy as the grid draws it. Title is what the person read when
// they kept it, retained rather than re-read, and SourceURL is the live record as
// answered by a read that reauthorized the caller for this row just now: an empty
// one is no link, whatever the retained values happen to mention.
type Row struct {
	ID           string
	Module       string
	Kind         string
	Key          string
	Title        string
	Captured     time.Time
	Availability string
	SourceURL    string
	Revision     int64
}

// Page is one page of one person's kept copies, and how many there are in all.
type Page struct {
	Rows  []Row
	Total int64
}

// Keeping is the saving capability as these two pages know it: their own kept rows,
// the keeping of one and the giving back of one. The implementation is the
// composition's adapter over the saving service, and it decides nothing of its own —
// the service re-reads the caller's person, their grants and the row's owner inside
// the caller's own transaction, so the route guard below is the door and never the
// authority. A record screen's own question — "is this row already kept" — is asked
// through ui/resource's SavingReader, which is the same adapter answering it.
type Keeping interface {
	Kept(ctx context.Context, module, kind string, limit, offset int) (Page, error)
	Keep(ctx context.Context, module, kind, key, expectedSourceVersion string) error
	GiveBack(ctx context.Context, id string, expectedRevision int64) error
}

// Options is what the shell decides about its own grid.
type Options struct {
	// PerPage is the composition's page size; 0 is defaultPerPage and a larger
	// number is bounded by the service's own cap. It is never read from a request,
	// so nobody can widen a page past what the composition decided to show.
	PerPage int
	// Workspace is the address the breadcrumb returns from.
	Workspace string
}

// Mount mounts the kept list, its one removal and the save a record screen posts,
// on the workspace surface and in the shell the composition passes. A composition
// that hands over nothing mounts nothing: the heart on a record and the list beside
// it are the same absent capability, absent the same way.
//
// All three routes are guarded by the signed-in rule alone, because whose copies
// these are is answered by the service in the caller's transaction, and no guard
// here could answer it more truly than the command does.
//
// It answers with the addresses it mounted, because the composition handing a record
// screen its control's targets needs the ones this mount actually created and not the
// ones it happened to guess.
func Mount(r *httpx.Router, s page.Shell, q Keeping, o Options) At {
	view := screen.MustView(screen.ViewSpec{Ref: "app/keeping/custom/saved-list", Cardinality: screen.Many})
	r = r.WithViews(view)
	perPage := o.PerPage
	if perPage <= 0 {
		perPage = defaultPerPage
	}
	at := mounts(r)
	page.Serve(r, s, page.Route{View: view.Ref(), ID: "app-saved", Method: http.MethodGet, Path: relList,
		Summary: "List the copies you kept",
		Errors:  []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, req page.Request, in *filters) (page.View, error) {
		// Two slugs and an offset: everything else the request carries is dropped
		// rather than carried forward, because a filter nothing reads is a filter a
		// link would go on promising.
		got, err := q.Kept(ctx, strings.TrimSpace(in.Module), strings.TrimSpace(in.Kind), perPage, max(in.Offset, 0))
		if err != nil {
			return page.View{}, err
		}
		return grid(req, s, o, at, got, *in, perPage), nil
	})

	page.Serve(r, s, page.Route{Exception: "redirect", Reason: "Giving a copy back redirects to the kept list.", ID: "app-saved-remove", Method: http.MethodPost, Path: relList + "/{id}/remove",
		Summary: "Give one kept copy back",
		Errors:  []int{http.StatusUnprocessableEntity, http.StatusConflict, http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, _ page.Request, in *removal) (page.View, error) {
		form, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return page.View{}, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
		}
		// A revision the form did not carry is 0, which the service answers as a
		// conflict for a copy that is present and as silence for one already given
		// back — the same two answers Unsave gives a JSON caller.
		revision, err := strconv.ParseInt(form.Get("expectedRevision"), 10, 64)
		if err != nil {
			revision = 0
		}
		if err := q.GiveBack(ctx, in.ID, revision); err != nil {
			return page.View{}, err
		}
		return page.View{}, httpx.SeeOther(gridAt(at, form.Get("module"), form.Get("kind"), 0))
	})

	page.Serve(r, s, page.Route{Exception: "redirect", Reason: "Keeping a copy redirects to the kept list.", ID: "app-saved-save", Method: http.MethodPost, Path: relList + "/save",
		Summary: "Keep a copy of a record",
		Errors:  []int{http.StatusUnprocessableEntity, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, _ page.Request, in *keeping) (page.View, error) {
		form, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return page.View{}, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
		}
		if err := q.Keep(ctx, form.Get("module"), form.Get("kind"), form.Get("key"),
			form.Get("expectedSourceVersion")); err != nil {
			return page.View{}, err
		}
		// To the kept list of this pair: it is the page that shows what the person
		// now holds, at an address this mount wrote. The body names no place to
		// return to — a submitted body is a caller's words, not an address to send
		// a caller off with, and no control this surface draws writes such a field.
		return page.View{}, httpx.SeeOther(gridAt(at, form.Get("module"), form.Get("kind"), 0))
	})
	return at
}

// filters is the grid's whole request: which pair to show, and how far into it.
type filters struct {
	Module string `query:"module"`
	Kind   string `query:"kind"`
	Offset int    `query:"offset"`
}

// removal is one submitted give-back: the copy's id from the path and, in the body,
// the revision the grid showed beside it and the pair it was filtered to.
type removal struct {
	ID      string `path:"id"`
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// keeping is one submitted save: the pair, the key the record published and the
// source version the record published beside it. Nothing
// else is submittable, because nothing else is this caller's to write — a title, a
// snapshot or a person in a body is refused as an unknown field rather than stored.
type keeping struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}
