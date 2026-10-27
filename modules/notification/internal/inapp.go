// The in-app list: the notices addressed to the person standing at the page.
//
// It is hand-drawn for the reason this module writes its two JSON routes by hand
// rather than generating them: a rest.Spec's list route is the whole tenant, and
// these rows are a person's — every caller's list is a different list. The page
// is therefore the same shape as those routes: one guard, one subject (the
// caller), and no recipient in the address or the query.
//
// What the page owns is the list and nothing else. The shell it is drawn in, the
// address its unread bell sits at and whether anybody is told at all are the
// composition's, which is why this takes a page.Shell and mounts nothing without
// one (see notification.Page). Marking a notice read stays the command it
// already is: a page that wrote from a link would be a second door onto the same
// command, and the row is the same row either way.
//
// The list is live: it names the two events that happen to one person's notices,
// and the kernel draws the region and the connection around it (document.View's
// Live). A notice says only that something changed, so a refresh is one ordinary
// read of this route — the same guarded, principal-scoped read the first paint
// made — and never whatever the frame happened to carry.
package internal

import (
	"context"
	"fmt"
	"net/http"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// InApp is the list page: what it reads, the shell it is drawn in, how many rows
// one page holds, and the events it is a live region for.
//
// A nil Read mounts nothing, which is the answer to a person who asks for a
// screen nobody composed: the surface's own 404, not an empty table.
type InApp struct {
	Shell   page.Shell
	Read    contracts.Service
	PerPage int
	// Topics are the events whose arrival makes this list read itself again. They
	// are the manifest's own declarations, handed down by the module rather than
	// written again here, so the door and the page cannot disagree about which
	// topics this page watches.
	Topics []string
}

// inAppPerPage is the screenful, the same 20 the generated lists and the change
// queue draw: a person moving between the hand-drawn screens and the generated
// ones should not learn a second pagination.
const inAppPerPage = 20

// inboxFaults are the statuses beyond the shell's defaults. Every one is
// kit/httpx's own answer to a request it cannot serve rather than to a row.
var inboxFaults = []int{http.StatusServiceUnavailable}

// MountInApp serves the caller's own notices at /notifications on the workspace
// surface, guarded by httpx.SignedIn like the two JSON routes beside it: the
// subject of the page is the caller, and a permission every signed-in person must
// hold to see their own list decides nothing.
func MountInApp(s httpx.Surfaces, p InApp) {
	if p.Read == nil || p.Shell.Frame == nil {
		return
	}
	per := p.PerPage
	if per <= 0 {
		per = inAppPerPage
	}
	// The address this page is served at, as the surface composes it. The module
	// owns the tail of the route ("/notifications"); the workspace namespace in
	// front of it belongs to the composition, and only the surface has met both.
	// The pager links this address and the live region re-reads it, so a person's
	// own page is one address and not two — and both answers come from the same
	// guarded handler, so the shape of the question changes nothing about what the
	// address reaches.
	mounted := s.App.PagePath(path)
	page.Serve(s.App, p.Shell, page.Route{
		View: contracts.InboxView, ID: "notification-inbox", Method: http.MethodGet, Path: path,
		Summary: "The notices addressed to you", Errors: inboxFaults,
	}, httpx.SignedIn(), func(ctx context.Context, r page.Request, in *inboxInput) (page.View, error) {
		return p.inbox(ctx, in.Page, per, mounted)
	})
}

// inbox reads this person's rows and draws them, newest first. The caller and
// the transaction come from the same reading the JSON list uses: the person is
// the principal the request authenticated, never a parameter.
func (p InApp) inbox(ctx context.Context, at, per int, mounted string) (page.View, error) {
	tx, me, err := caller(ctx)
	if err != nil {
		return page.View{}, err
	}
	at = max(at, 1)
	rows, total, err := p.Read.ListFor(ctx, tx, me, crud.Query{Limit: per, Offset: (at - 1) * per})
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	last := max(int((total+int64(per)-1)/int64(per)), 1)
	return page.View{
		Title: "Notifications",
		Live:  p.Topics,
		Body: []g.Node{
			components.Toolbar(components.ToolbarProps{
				Title:    "Notifications",
				Subtitle: fmt.Sprintf("%d notice%s, newest first. Only the person a notice is addressed to sees it here.", total, plural(total)),
			}),
			inboxTable(rows),
			components.Pagination(components.PaginationProps{
				// The mounted address, never the bare collection: /notifications is
				// the JSON route's address and no host serves a page there, so a
				// pager written with the tail linked a person over one screenful of
				// notices to a 404.
				CurrentPage: min(at, last), TotalPages: last, BaseURL: mounted,
				NavigationLabel: "Notifications pagination",
			}),
		},
	}, nil
}

// inboxTable is the list. A person with nothing to read is told so where the
// table would have been, rather than shown an empty frame.
func inboxTable(rows []*contracts.Notification) g.Node {
	if len(rows) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title:       "Nothing to read",
			Description: "No notice has been addressed to you.",
			Bordered:    true,
		})
	}
	return components.TableWithSlots(
		components.TableProps{
			Columns: []components.TableColumn{
				{Key: "title", Label: "Notice", Primary: true},
				{Key: "detail", Label: "Details"},
				{Key: "state", Label: "State"},
				{Key: "when", Label: "When"},
			},
			Rows:  inboxRows(rows),
			Label: "Your notifications",
		},
		components.TableSlots{Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			text, _ := row.Cells[c.Key].(string)
			if c.Key != "title" {
				return g.Text(text)
			}
			link, _ := row.Cells["link"].(string)
			if link == "" {
				return g.Text(text)
			}
			return h.A(h.Href(link), g.Text(text))
		}},
	)
}

// inboxRows is one line per notice: what it said, whether it was read, and when
// it arrived. A notice whose link is not a path within this application is drawn
// as text — the entity refuses to store one, and the page does not trust that it
// was checked.
func inboxRows(rows []*contracts.Notification) []components.TableRow {
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		link := ""
		if httpx.LocalPath(row.Link) {
			link = row.Link
		}
		state := "unread"
		if row.ReadAt != nil {
			state = "read"
		}
		out = append(out, components.TableRow{
			ID: row.ID.String(),
			Cells: map[string]any{
				"title":  row.Title,
				"detail": row.Body,
				"state":  state,
				"when":   stamp(row.CreatedAt),
				"link":   link,
			},
		})
	}
	return out
}

// stamp is the one timestamp format these hand-drawn pages use: UTC, to the
// second, which is what a notice's ordering is decided on.
func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// plural is the subtitle's one grammar rule.
func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// inboxInput is the page a person asked for, one-based, so the pager's links read
// as addresses and not as offsets.
type inboxInput struct {
	Page int `query:"page" default:"1" minimum:"1" doc:"Which page of notices to show"`
}
