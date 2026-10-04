package internal

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// Sessions is what the shell needs of the auth module: the list a person reads
// and the two ways out of it. Narrower than authcontracts.Service for the reason
// Roles is — a consumer depends on the capability it uses — and it is the same
// three commands the module's JSON routes answer with, not a third way to end a
// session. A session is named by its ref and never an id, which is
// contracts.SessionListing's rule and the reason the form below hides it.
type Sessions interface {
	// Sessions is this person's live sessions, newest first, with the one
	// answering the request marked current.
	Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*authcontracts.SessionListing, error)
	// RevokeSession ends the session this person names by its ref.
	RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error
	// RevokeSessions ends all of them except the one named, which is how the
	// screen offers "everywhere but here" without inventing a third command.
	RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error
}

// sessionsWriteFaults is what the two revocations can answer with besides the
// shell's own set: a ref that is not one of this person's live sessions is a
// 404 whoever wrote it, and it is the same 404 as a session that was never here.
var sessionsWriteFaults = []int{http.StatusNotFound, http.StatusServiceUnavailable}

// sessionForm is one revocation: the ref of the session to end, carried as a
// value the person never reads. Like roleForm it is read through
// url.ParseQuery by the handler rather than declared as a body schema, because
// a form written by hand posts exactly the fields the route underneath needs.
type sessionForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// mountSessions is the seventh hand-written page, for the reason the other six
// are here: this module is composed last and owns the shell, so it is the only
// one that can put a screen inside the chrome. The screen belongs to modules/auth
// — its commands answer it — and the rendering to the shell, which is the split
// the roles screen established. Unlike that one it carries no nav entry:
// kit/module.Validate refuses an entry naming no permission, and a product that
// wants the link names it beside a permission it seeds.
//
// The guard is SignedIn and no permission, which is the whole of the point:
// these are the caller's own sessions, and a person who may not read anybody
// else's row may still see the machines they themselves are signed in on. The
// commands take the caller's id from the credential, never from the request, so
// there is nothing here for a crafted form to aim at another person.
func (p pages) mountSessions(app *httpx.Router) {
	if p.Sessions == nil {
		return
	}
	guard := httpx.SignedIn()
	where := namespace(app, "auth")

	page.Serve(where, p.shell, page.Route{ID: "admin-sessions", Method: http.MethodGet, Path: p.at.sessions.rel,
		Summary: "The sessions this person has", Errors: sessionsReadFaults}, guard,
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return p.sessionsView(ctx, r)
		})

	// POST rather than DELETE, at the address the module's own revocation route
	// uses the same verb for: kit/httpx's CSRF rule refuses an unsafe request
	// carrying a session cookie unless it is same-site, and a <form> cannot send
	// DELETE. The JSON route stays as it is for a client that can.
	page.Serve(where, p.shell, page.Route{ID: "admin-session-revoke", Method: http.MethodPost, Path: p.at.sessionRevoke.rel,
		Summary: "End one of this person's sessions", Errors: sessionsWriteFaults,
		IdempotencyKey: true}, guard,
		func(ctx context.Context, r page.Request, in *sessionForm) (page.View, error) {
			tx, live := httpx.TxFrom(ctx)
			if !live {
				return page.View{}, unreachable
			}
			ref, err := formValue(in.RawBody, "ref")
			if err != nil {
				return page.View{}, err
			}
			if err := p.Sessions.RevokeSession(ctx, tx, r.Principal.UserID, ref); err != nil {
				return page.View{}, rest.Fault(err)
			}
			return page.View{}, httpx.SeeOther(p.at.sessions.at)
		})

	page.Serve(where, p.shell, page.Route{ID: "admin-sessions-revoke-rest", Method: http.MethodPost, Path: p.at.sessionsRest.rel,
		Summary: "End every session but this one", Errors: sessionsWriteFaults,
		IdempotencyKey: true}, guard,
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			tx, live := httpx.TxFrom(ctx)
			if !live {
				return page.View{}, unreachable
			}
			// The session making the request is kept, because ending it from the
			// page would sign the person out mid-click and leave them reading a
			// 401 instead of the list they asked for. "Everywhere but here" is
			// what the button says and what the command does; the row this person
			// is reading on is revoked by its own button, deliberately.
			if err := p.Sessions.RevokeSessions(ctx, tx, r.Principal.UserID, currentSession(ctx)); err != nil {
				return page.View{}, rest.Fault(err)
			}
			return page.View{}, httpx.SeeOther(p.at.sessions.at)
		})
}

// sessionsReadFaults is what the list answers with when it cannot be drawn: the
// database, and nothing else — a person always has the right to their own list.
var sessionsReadFaults = []int{http.StatusServiceUnavailable}

// sessionsView reads this person's sessions and draws them.
func (p pages) sessionsView(ctx context.Context, r page.Request) (page.View, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unreachable
	}
	items, err := p.Sessions.Sessions(ctx, tx, r.Principal.UserID, currentSession(ctx))
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	return sessionsPage(p.at.sessions.at, p.at.sessionRevoke.at, p.at.sessionsRest.at, items), nil
}

// currentSession is the session answering this request, or uuid.Nil.
//
// It is read from the cookie the request already presented and parsed as the id
// it is, because the kernel's Principal carries the person and not the session —
// and it is the same computation the auth module does with its own cookie,
// reached through contracts.SessionRefMatches' counterpart: the cookie holds the
// id, the table holds hash(id). Marking a row current is a display fact, so
// nothing here decides anything with it; a request with no parsable cookie
// simply shows nothing as "this device".
func currentSession(ctx context.Context) uuid.UUID {
	r, ok := httpx.RequestFrom(ctx)
	if !ok {
		return uuid.Nil
	}
	cookie, ok := httpx.SessionCookieOf(r)
	if !ok {
		return uuid.Nil
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// sessionsPage is the screen as a function of values: what is there, and the
// three addresses the forms post to.
func sessionsPage(where, revokeOne, revokeRest string, items []*authcontracts.SessionListing) page.View {
	body := []g.Node{components.Toolbar(components.ToolbarProps{
		Title:    "Sessions",
		Subtitle: "Every place you are signed in, most recently seen first. Ending one signs that device out; it does not change your password.",
	})}
	if len(items) == 0 {
		// No rows is not an error and not an empty table: it is the answer to
		// "was that me?" when there is nothing to ask about.
		body = append(body, components.EmptyState(components.EmptyStateProps{
			Title:       "Nothing is signed in",
			Description: "This request opened no session, so there is nothing here to end.",
			Bordered:    true,
		}))
		return page.View{Title: "Sessions", Language: writtenHere, Body: body}
	}
	body = append(body, sessionsTable(revokeOne, items))
	// The count is the rows that are not this one, counted where they are. The
	// command ends whatever the caller's own session is, wherever it sits, and the
	// order this list arrives in is last_seen_at — a column the module slides at
	// most once per SessionTouch, so the reading session legitimately sorts behind
	// one used more recently. Peeking at the head of the list would then name one
	// more ending than the click performs, and the person would have agreed to a
	// different number than the one that happened.
	rest := 0
	for _, s := range items {
		if !s.Current {
			rest++
		}
	}
	if rest > 0 {
		body = append(body, components.Divider(components.DividerProps{Text: "Everywhere else"}),
			components.Form(components.FormProps{Action: revokeRest, Label: "End every session but this one", HTMXProps: components.HTMXProps{Ext: "command"}},
				components.FormActions(components.FormActionsProps{},
					components.Button(components.ButtonProps{
						Label: "End the other " + strconv.Itoa(rest), Type: "submit", Tone: "danger"}))))
	}
	return page.View{Title: "Sessions", Language: writtenHere, Body: body}
}

// sessionsTable is the answer as one table: the device, when it was opened, when
// it was last used, when it ends, where from, and the button that ends it.
//
// It is a table for the reason the shell's own lists are tables — the question is
// "was that me?" asked of several machines at once, and a person answers it by
// comparing columns down a page, not by reading one paragraph per machine. Card
// per session put every row's text on its own left edge and its own body size,
// so the page the person actually reads was indented three times and set in four
// sizes; the roles and tenants screens above never did. The one button that is
// allowed to be loud on this page is the one the toolbar describes, and it is
// drawn once: a list of fifteen sessions would otherwise be fifteen red fills
// competing for the same click.
//
// The ref is a hidden value and never a cell, which is the module's rule for the
// list spelled out in contracts.SessionListing — a list a person reads off their
// own screen must not read like a sheet of live credentials. The device string is
// what the browser said, clipped by the module to 400 characters, and an empty
// one says so rather than rendering a blank row.
func sessionsTable(revokeOne string, items []*authcontracts.SessionListing) g.Node {
	rows := make([]components.TableRow, 0, len(items))
	for _, s := range items {
		rows = append(rows, components.TableRow{ID: s.Ref, Cells: map[string]any{
			"device":    deviceNamed(s),
			"current":   s.Current,
			"signed_in": when(s.CreatedAt),
			"last_used": when(s.LastSeenAt),
			"ends":      when(s.ExpiresAt),
			"from":      s.IP,
		}})
	}
	return components.TableWithSlots(components.TableProps{
		Columns: []components.TableColumn{
			{Key: "device", Label: "Device", Primary: true},
			{Key: "signed_in", Label: "Signed in"},
			{Key: "last_used", Label: "Last used"},
			{Key: "ends", Label: "Ends"},
			{Key: "from", Label: "From"},
			{Key: "end", Label: "End", Align: "right"},
		},
		Rows:  rows,
		Label: "Your sessions, most recently seen first",
	}, components.TableSlots{
		Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			device := rest.Text(row.Cells["device"])
			switch c.Key {
			case "device":
				if row.Cells["current"] != true {
					return nil
				}
				return components.Flex(components.FlexProps{Gap: "2", Align: "center"},
					g.Text(device),
					components.Badge(components.BadgeProps{
						Label: "This device", Tone: "success", Variant: "outline"}))
			case "from":
				if rest.Text(row.Cells["from"]) == "" {
					// The session recorded no address: the row says so rather than
					// leaving a blank somebody has to read as a missing value.
					return g.Text("—")
				}
				return nil
			case "end":
				// The form belongs to the row it describes, so the button posts one
				// ref and a person who tabs to it hears which session they are about
				// to end. It is quiet on purpose, and on this page quiet has to mean
				// unfilled in either palette: the design probe calls a control filled
				// when its own background is opaque and its channels sum under 600, and
				// surface-primary — what the secondary variant paints — is #fffdf7 in the
				// light theme and #151f1d in the dark one. A table of secondary row
				// buttons is therefore one thing to do under a light scheme and four
				// under a dark one, while the floor ("one thing to do per view") is
				// about what a person sees first and not about which media query their
				// browser matched. So this row carries no fill at all, and the page
				// paints one filled control above the fold whoever is looking at it.
				return components.Form(components.FormProps{Action: revokeOne, Label: "End the session on " + device, HTMXProps: components.HTMXProps{Ext: "command"}},
					components.Input(components.InputProps{
						Type: "hidden", Name: "ref", Value: rest.Text(row.ID)}),
					components.Button(components.ButtonProps{
						Label: "End this session", Type: "submit", Variant: "ghost",
						AriaLabel: "End the session on " + device}))
			}
			return nil
		},
	})
}

// deviceNamed is the row's headline: what the browser said, or the truth about a
// session that arrived without a user agent.
func deviceNamed(s *authcontracts.SessionListing) string {
	if s.UserAgent == "" {
		return "A device that would not say what it is"
	}
	return s.UserAgent
}

// formValue is one field of a hand-written form, and the refusal when the body
// is not a form at all. A ref that arrives empty is not special-cased here: the
// command refuses it as the unknown ref it is, and the screen says the same
// thing about a missing one and a made-up one.
func formValue(raw []byte, key string) (string, error) {
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		return "", problem.New(http.StatusUnprocessableEntity, "this form could not be read")
	}
	return form.Get(key), nil
}

// when is a moment a person can read: to the minute, in UTC, with the zone said
// rather than assumed. The stored time is already UTC; the label is what stops
// somebody reading "last used 09:14" as their own morning.
func when(t time.Time) string {
	return t.UTC().Format("2 January 2006, 15:04") + " UTC"
}
