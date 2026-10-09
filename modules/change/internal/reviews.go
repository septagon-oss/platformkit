// The two screens this module owns: the queue of what is waiting for a decision, and the
// one proposal a reviewer decides from. They live in this package and not in a ui
// sub-package for the reason modules/admin's seven hand-drawn pages live in its own
// internal — one package per module's implementations, and the page layer is the
// foundation's (ui/page, ui/components), which this file uses and does not become.
//
// They are hand-drawn rather than generated because a proposal is not a resource a
// rest.Spec describes — five routes on a collection, three of which write whatever the body
// says, over an object whose state moves only through four named commands with an actor rule
// each. The module's README has said that about its JSON routes since the module existed; the
// same sentence is why its screens are written rather than mounted.
//
// What the module owns is the two pages below and the decision controls on them. What it does
// not own is the shell they are drawn in, the dashboard around them, or any notice about them:
// the composition hands in the page.Shell it already builds, and the notice is a port the
// composition implements over whatever it delivers with (contracts.Notifier). A capability that
// knew which chrome its queue sat in — or that a mail server existed — would be a capability
// with a product in it.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// Reader is the queue's whole need of the service: the list, and one row of it.
type Reader interface {
	List(ctx context.Context, tx db.Tx[db.Tenant], q contracts.Query) ([]*contracts.Proposal, int64, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Proposal, error)
}

// Decider is the commands the page offers. Withdraw is among them although the page
// draws it only while the row is open, because a button that appears is a convenience
// and the check that decides whose proposal it is lives in the module.
type Decider interface {
	Review(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in contracts.Review) (*contracts.Proposal, error)
	Apply(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*contracts.Proposal, error)
	Withdraw(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*contracts.Proposal, error)
}

// Pages is the two screens: what they read, what they write, the shell they are drawn
// in, and — for the decision controls — whose answer to "may this caller decide" the
// page reuses.
//
// A nil Read mounts nothing at all. A composition that wires change control's doors and
// no queue is an ordinary installation — the module worked that way before these two
// pages existed — and the honest answer to a person who asks for a screen nobody composed
// is the surface's own 404, not an empty table.
//
// A nil Authorize draws the controls for anybody who reached the page: the guard on the
// post is what decides, and a page that drew no controls because nobody had wired the
// question would be a page that refuses its own subject.
type Pages struct {
	Shell     page.Shell
	Read      Reader
	Decide    Decider
	Authorize httpx.Authorizer
	PerPage   int
}

// queueFaults and decisionFaults are the statuses the two pages answer with beyond the
// shell's defaults. Every one is kit/rest's mapping of an error the service already
// returns: another tenant's proposal, a row in a state that allows no such command, an
// expected revision the row has passed.
var (
	queueFaults    = []int{http.StatusServiceUnavailable}
	decisionFaults = []int{
		http.StatusNotFound, http.StatusConflict,
		http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
	}
)

// defaultPerPage is the screenful, the same 20 the generated lists draw: a person
// moving between the generated screens and this one should not learn a second pagination.
const defaultPerPage = 20

// historyRows is how many of a row's other proposals the page draws. A screenful, and
// not everything: the trail behind a proposal is audit's, at its own door.
const historyRows = 20

// Mount serves the queue and the proposal page, and the three decision posts.
//
// The guard is the module's own permission on every one of them — change:read to look,
// change:decide to decide or apply, change:propose to withdraw a proposal of one's own —
// asked by the router before a line is rendered. A watcher therefore sees no decision
// controls because the page was composed for a person who may not press them, which is a
// different claim from a page that built them and hid them.
func MountReviews(s httpx.Surfaces, p Pages) {
	if p.Read == nil {
		return
	}
	where := s.App.PagePath("/proposals")
	read := httpx.Permission(contracts.PermissionChangeRead)
	propose := httpx.Permission(contracts.PermissionChangePropose)
	decide := httpx.Permission(contracts.PermissionChangeDecide)

	page.Serve(s.App, p.Shell, page.Route{ID: "change-proposal-queue", Method: http.MethodGet,
		Path: "/proposals", Summary: "The changes waiting for a decision", Errors: queueFaults}, read,
		func(ctx context.Context, r page.Request, in *queueInput) (page.View, error) {
			return p.queue(ctx, r, where, in.Page)
		})

	page.Serve(s.App, p.Shell, page.Route{ID: "change-proposal-read", Method: http.MethodGet,
		Path: "/proposals/{id}", Summary: "One proposed change, to decide", Errors: decisionFaults}, read,
		func(ctx context.Context, r page.Request, in *proposalInput) (page.View, error) {
			return p.detail(ctx, r, where, in.ID, "")
		})

	// POST rather than the JSON verb, for the reason modules/admin's session revocation
	// gives: kit/httpx refuses an unsafe request carrying a session cookie unless it is
	// same-site, and a <form> cannot send the verb the API route uses. The JSON commands
	// stay exactly as they are for a client that can.
	page.Serve(s.App, p.Shell, page.Route{ID: "change-proposal-review", Method: http.MethodPost,
		Path: "/proposals/{id}/review", Summary: "Approve or decline a proposed change",
		Errors: decisionFaults}, decide, func(ctx context.Context, r page.Request, in *decisionForm) (page.View, error) {
		sent, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return p.detail(ctx, r, where, in.ID, "the form could not be read")
		}
		expected, err := expectedRevision(sent)
		if err != nil {
			return p.detail(ctx, r, where, in.ID, err.Error())
		}
		return p.run(ctx, r, where, in.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := p.Decide.Review(ctx, tx, in.ID, contracts.Review{
				Verdict: sent.Get("verdict"), Comment: sent.Get("comment"),
				ExpectedRevision: expected,
			})
			return err
		})
	})

	page.Serve(s.App, p.Shell, page.Route{ID: "change-proposal-apply", Method: http.MethodPost,
		Path: "/proposals/{id}/apply", Summary: "Apply an approved change",
		Errors: decisionFaults}, decide, func(ctx context.Context, r page.Request, in *decisionForm) (page.View, error) {
		sent, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return p.detail(ctx, r, where, in.ID, "the form could not be read")
		}
		expected, err := expectedRevision(sent)
		if err != nil {
			return p.detail(ctx, r, where, in.ID, err.Error())
		}
		return p.run(ctx, r, where, in.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := p.Decide.Apply(ctx, tx, in.ID, expected)
			return err
		})
	})

	page.Serve(s.App, p.Shell, page.Route{ID: "change-proposal-withdraw", Method: http.MethodPost,
		Path: "/proposals/{id}/withdraw", Summary: "Take a proposal back",
		Errors: decisionFaults}, propose, func(ctx context.Context, r page.Request, in *decisionForm) (page.View, error) {
		sent, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return p.detail(ctx, r, where, in.ID, "the form could not be read")
		}
		expected, err := expectedRevision(sent)
		if err != nil {
			return p.detail(ctx, r, where, in.ID, err.Error())
		}
		return p.run(ctx, r, where, in.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := p.Decide.Withdraw(ctx, tx, in.ID, expected)
			return err
		})
	})
}

// queueInput is the one thing the list takes: which screenful.
type queueInput struct {
	Page int `query:"page" default:"1" minimum:"1" doc:"Which screenful of proposals"`
}

// proposalInput is the item page's path parameter.
type proposalInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The proposal"`
}

// decisionForm is one decision: which row, and the body the person posted. Like every
// hand-drawn form in the kernel it is read as url.Values by the handler rather than
// declared as a body schema, because a form written by hand posts exactly the fields the
// command underneath needs and no second vocabulary for them.
type decisionForm struct {
	ID      uuid.UUID `path:"id" format:"uuid" doc:"The proposal"`
	RawBody []byte
}

// expectedRevision is the number the form was drawn with. It is asked for, not assumed:
// a form that posted no revision would leave the command to choose one, and the choice
// that makes two reviewers into one decision is the number the person looked at.
func expectedRevision(sent url.Values) (int64, error) {
	text := strings.TrimSpace(sent.Get("expectedRevision"))
	at, err := strconv.ParseInt(text, 10, 64)
	if err != nil || at < 1 {
		return 0, fmt.Errorf("this form carries no revision to write against, so nothing was decided")
	}
	return at, nil
}

// run is one command, answered the way every hand-drawn write here answers: the person
// who succeeded is sent back to the row with SeeOther, so reloading cannot repeat the
// command, and the person who was refused is given the proposal again with the refusal
// above it, in the words of the module that refused.
func (p Pages) run(ctx context.Context, r page.Request, where string, id uuid.UUID,
	command func(context.Context, db.Tx[db.Tenant]) error) (page.View, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unavailable
	}
	if p.Decide == nil {
		// No commands wired. The page drew no controls, so whatever reached this door was
		// not the page's own form, and the answer that says so is the one about there
		// being no such door rather than one about a row.
		return page.View{}, problem.NotFound("this application offers no decision controls")
	}
	if err := command(ctx, tx); err != nil {
		return p.detail(ctx, r, where, id, refusal(err, r.Locale))
	}
	return page.View{}, httpx.SeeOther(where + "/" + id.String())
}

// queue lists the open work and links every row to the page that decides it.
func (p Pages) queue(ctx context.Context, r page.Request, where string, at int) (page.View, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unavailable
	}
	per := p.perPage()
	at = max(at, 1)
	rows, total, err := p.Read.List(ctx, tx, contracts.Query{Limit: per, Offset: (at - 1) * per})
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	last := max(int((total+int64(per)-1)/int64(per)), 1)
	return page.View{Title: "Change reviews", Body: []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: "Change reviews",
			Subtitle: fmt.Sprintf("%d proposal%s, newest first. Each one waits for an account other than the person who wrote it.",
				total, plural(total)),
		}),
		queueTable(where, rows),
		components.Pagination(components.PaginationProps{
			CurrentPage: min(at, last), TotalPages: last, BaseURL: where,
			NavigationLabel: "Change reviews pagination",
		}),
	}}, nil
}

// queueTable is the list. A tenant with nothing open is told so where the table would
// have been, rather than shown an empty frame.
func queueTable(where string, rows []*contracts.Proposal) g.Node {
	if len(rows) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title:       "Nothing is waiting",
			Description: "No change in this tenant is open for a decision.",
			Bordered:    true,
		})
	}
	return components.TableWithSlots(
		components.TableProps{
			Columns: []components.TableColumn{
				{Key: "summary", Label: "Change", Primary: true},
				{Key: "subject", Label: "Subject"},
				{Key: "state", Label: "State"},
				{Key: "base", Label: "Against revision", Align: "right"},
				{Key: "applied", Label: "Applied revision", Align: "right"},
			},
			Rows:  rowsOf(rows),
			Label: "Change proposals",
		},
		tableLinks(where))
}

// tableLinks makes the summary the row's address. The summary is what a person reads to
// decide which proposal to open, so it is the thing they activate: a table whose rows are
// text and whose link is an unlabelled icon in a column nobody named is a table a
// keyboard lands on the wrong thing in.
func tableLinks(where string) components.TableSlots {
	return components.TableSlots{Cell: func(row components.TableRow, c components.TableColumn) g.Node {
		text, _ := row.Cells[c.Key].(string)
		if c.Key != "summary" {
			return g.Text(text)
		}
		return h.A(h.Href(where+"/"+row.ID), g.Text(text))
	}}
}

// detail is one proposal: what it would write, what was decided about it, what else has
// been proposed for the same row, and the controls this caller may use.
//
// refusal is what a command the person just tried said about itself, or "" for a page
// nobody was refused on. It is drawn above everything else, because a decision that
// failed is the thing they came back to read.
func (p Pages) detail(ctx context.Context, r page.Request, where string, id uuid.UUID, refusal string) (page.View, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unavailable
	}
	row, err := p.Read.Get(ctx, tx, id)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	others, _, err := p.Read.List(ctx, tx, contracts.Query{
		SubjectModule: row.SubjectModule, SubjectEntity: row.SubjectEntity,
		SubjectID: row.SubjectID, Limit: historyRows,
	})
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	status := 0
	body := []g.Node{}
	if refusal != "" {
		// The status of a page that refused you is the status of the refusal, so a
		// reload of the answer says what the click did.
		status = http.StatusConflict
		body = append(body, components.Alert(components.AlertProps{
			Tone: "danger", Title: refusalTitle(r.Locale), Message: refusal, Bordered: true}))
	}
	may, err := p.mayDecide(ctx, r)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	body = append(body,
		components.DetailList(components.DetailListProps{
			Title:        row.Summary,
			Description:  "Proposed against " + row.SubjectModule + "/" + row.SubjectEntity + ", revision " + strconv.FormatInt(row.BaseRevision, 10) + ".",
			SemanticRole: "change-proposal",
			Items:        facts(row),
		}),
		difference(row),
		decisions(where, row, may),
		history(where, id, others),
	)
	return page.View{Title: row.Summary, Status: status, Body: body}, nil
}

// facts is the row as a person reads it: its state, what it is about, the decision and
// who made it, and what the row is on now. Every value is the row's own; nothing here is
// inferred, and no second ledger is drawn.
func facts(row *contracts.Proposal) []components.DetailItem {
	out := []components.DetailItem{
		{Label: "State", Value: stateLabel(row.State)},
		{Label: "Subject", Value: row.SubjectModule + "/" + row.SubjectEntity + " " + row.SubjectID.String()},
		{Label: "Proposed against revision", Value: strconv.FormatInt(row.BaseRevision, 10)},
		{Label: "Diff digest", Value: row.DiffDigest},
		{Label: "This proposal's revision", Value: strconv.FormatInt(row.Revision, 10)},
	}
	if row.Verdict != "" {
		out = append(out, components.DetailItem{Label: "Verdict", Value: row.Verdict + ", " + when(row.ReviewedAt)})
	}
	if row.Comment != "" {
		out = append(out, components.DetailItem{Label: "Comment", Value: row.Comment})
	}
	if row.State == contracts.StateApplied {
		out = append(out, components.DetailItem{
			Label: "Applied as revision", Value: strconv.FormatInt(row.AppliedRevision, 10),
		})
	}
	return out
}

// difference draws what the proposal would write: one line per changed field, with the
// exact value the apply sets.
//
// The reviewed bytes and the applied bytes are the same bytes — the digest on the page
// above is that claim — so the diff is the whole of what a reviewer is asked about. What
// the field holds *now* is the subject's own screen, named by the subject line above:
// this page does not read another module's row to print a second number, which is what
// keeps it true to one second.
func difference(row *contracts.Proposal) g.Node {
	fields := slices.Sorted(maps.Keys(row.Diff))
	if len(fields) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title: "The diff is empty", Description: "This proposal would write nothing.",
			Compact: true, Bordered: true,
		})
	}
	items := make([]components.DetailItem, 0, len(fields))
	for _, field := range fields {
		items = append(items, components.DetailItem{Label: field, Value: written(row.Diff[field])})
	}
	return components.DetailList(components.DetailListProps{
		Title: "The change it would make", Description: "The value each field is set to, as reviewed.",
		SemanticRole: "change-difference", Items: items,
	})
}

// decisions is the approve, decline, apply and withdraw forms.
//
// Three rules are visible in the markup and all three are load-bearing. Every form
// carries the revision the page was drawn from as a hidden value, so two people deciding
// one proposal is one decision and one refusal that says so — not whichever click
// arrived last being the one nobody read. The controls are real <form method="post">
// elements with real submit buttons in reading order, so a phone with no script, a
// keyboard with no pointer and a screen reader with no styling all get the same two
// decisions. And a caller who may not decide is given no form at all.
//
// Which control a state answers with is the command underneath it, read from the
// service: a proposed row is decided, an approved row is applied, and a second verdict
// on a row somebody already approved is a button that cannot change anything — Review
// refuses it as a row "already approved", so drawing it would offer the person a
// sentence about their own click instead of the write they came to make. The approved
// branch therefore comes first, and the two are never drawn together.
func decisions(where string, row *contracts.Proposal, mayDecide bool) g.Node {
	at := where + "/" + row.ID.String()
	open := row.State == contracts.StateProposed || row.State == contracts.StateApproved
	body := []g.Node{h.H2(g.Attr("id", "change-decision"), g.Text("Decision"))}
	switch {
	case mayDecide && row.State == contracts.StateApproved:
		body = append(body, postForm(at+"/apply", row.Revision, "Apply the approved change",
			h.Button(h.Type("submit"), g.Text("Apply"))))
	case mayDecide && open:
		body = append(body,
			postForm(at+"/review", row.Revision, "Approve this change",
				hidden("verdict", "approved"),
				h.Button(h.Type("submit"), g.Text("Approve"))),
			postForm(at+"/review", row.Revision, "Decline this change",
				hidden("verdict", "declined"),
				commentField(at),
				h.Button(h.Type("submit"), g.Text("Decline"))),
		)
	case open:
		// A watcher watches. The controls are absent rather than disabled, because a
		// control that cannot be used is a fact about the page, not about the change.
		body = append(body, h.P(g.Text("Only an account holding change:decide, other than the one that proposed this, can decide it.")))
	default:
		body = append(body, h.P(g.Text("This proposal is "+stateLabel(row.State)+" and takes no further decision.")))
	}
	if mayDecide && open {
		// Withdraw is the proposer's, and the page draws it for anybody who may propose
		// and lets the command refuse: who wrote a proposal is a fact the row holds, and
		// a page that guessed would be a page with its own actor rule.
		body = append(body, postForm(at+"/withdraw", row.Revision, "Take this proposal back",
			h.Button(h.Type("submit"), g.Text("Withdraw"))))
	}
	return h.Section(g.Attr("aria-labelledby", "change-decision"), h.Div(body...))
}

// postForm is one decision: a real form, in reading order, carrying the revision it
// quotes so the command can refuse a decision made against a row the reader never saw.
func postForm(action string, revision int64, label string, fields ...g.Node) g.Node {
	return h.Form(
		h.Action(action), h.Method("post"), g.Attr("aria-label", label),
		h.Input(h.Type("hidden"), h.Name("expectedRevision"), h.Value(strconv.FormatInt(revision, 10))),
		h.Div(fields...),
	)
}

// hidden is a value the person never reads and the command cannot do without.
func hidden(name, value string) g.Node {
	return h.Input(h.Type("hidden"), h.Name(name), h.Value(value))
}

// commentField is the optional sentence the reviewer writes. Its bound is the entity's
// own: contracts.Proposal.Comment carries maxLength 2000, and a form that let a person
// type past the limit the row refuses is a form that refuses them twice.
func commentField(at string) g.Node {
	return h.Div(
		h.Label(g.Attr("for", at+"-comment"), g.Text("Comment")),
		h.Input(h.Type("text"), h.Name("comment"), g.Attr("id", at+"-comment"),
			h.MaxLength("2000"), h.Placeholder("Why, in the reviewer's own words")),
	)
}

// history is the other proposals made about the same row, newest first, from the list
// the service already answers by subject. It is not an audit trail and does not pretend
// to be one: the events are audit's, at audit's own door.
func history(where string, mine uuid.UUID, rows []*contracts.Proposal) g.Node {
	var others []*contracts.Proposal
	for _, row := range rows {
		if row.ID != mine {
			others = append(others, row)
		}
	}
	at := strings.TrimSuffix(where+"/"+mine.String(), "/"+mine.String())
	if len(others) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title:       "Nothing else proposed for this row",
			Description: "This is the first change put forward for it through change control.",
			Compact:     true, Bordered: true,
		})
	}
	return h.Section(
		h.H2(g.Text("Other proposals for this row")),
		components.TableWithSlots(
			components.TableProps{
				Columns: []components.TableColumn{
					{Key: "summary", Label: "Change", Primary: true},
					{Key: "state", Label: "State"},
					{Key: "base", Label: "Against revision", Align: "right"},
				},
				Rows:  rowsOf(others),
				Label: "Other proposals for this row",
			},
			tableLinks(at)),
	)
}

// rowsOf spells one proposal as the columns every list of them draws.
func rowsOf(rows []*contracts.Proposal) []components.TableRow {
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		applied := "—"
		if row.State == contracts.StateApplied {
			applied = strconv.FormatInt(row.AppliedRevision, 10)
		}
		out = append(out, components.TableRow{
			ID: row.ID.String(),
			Cells: map[string]any{
				"summary": row.Summary,
				"subject": row.SubjectModule + "/" + row.SubjectEntity,
				"state":   stateLabel(row.State),
				"base":    strconv.FormatInt(row.BaseRevision, 10),
				"applied": applied,
			},
		})
	}
	return out
}

// mayDecide asks the composition's own authorizer, which is the one that enforces every
// route in the application. A page with its own rules about who looks like a reviewer is a
// page that lies in one direction or the other, so this one asks the same question the
// guard would and draws what the answer says.
func (p Pages) mayDecide(ctx context.Context, r page.Request) (bool, error) {
	if p.Authorize == nil {
		return r.SignedIn, nil
	}
	ok, err := p.Authorize.Allowed(ctx, r.Tenant, tenancy.Grant{Permission: contracts.PermissionChangeDecide})
	if err != nil {
		// The question failed, so the controls cannot be drawn truthfully either way.
		// Refusing the page is honest; drawing a decision the person cannot make is not.
		return false, err
	}
	return ok, nil
}

// perPage is the screenful, defaulted once.
func (p Pages) perPage() int {
	if p.PerPage > 0 {
		return p.PerPage
	}
	return defaultPerPage
}

// stateLabel is the state as a person reads it. The five machine names stay in the
// digest, the events and the query parameter; this is the one place they become words.
func stateLabel(state string) string {
	switch state {
	case contracts.StateProposed:
		return "waiting for a decision"
	case contracts.StateApproved:
		return "approved, not applied"
	case contracts.StateDeclined:
		return "declined"
	case contracts.StateWithdrawn:
		return "withdrawn by its proposer"
	case contracts.StateApplied:
		return "applied"
	default:
		return state
	}
}

// plural is the one grammar the subtitle needs.
func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// when is a timestamp the way the page reads it, and "not yet" for the nil every open
// row carries.
func when(at *time.Time) string {
	if at == nil {
		return "not yet"
	}
	return at.UTC().Format(time.RFC3339)
}

// written is one diff entry as a line of text: the bytes the apply will write, through
// the same encoder the digest was taken over.
func written(raw any) string {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Sprintf("%v", raw)
	}
	return string(encoded)
}

// refusal is the sentence a refused command leaves on the page, in the language the
// request was answered in. The module owns these refusals, so it owns their copy: the
// English is the service's own line, passed as the readable fallback, and the catalogue
// in modules/change/messages answers the key in another language (decision 0012 rule 2 —
// the same shape ui/page/fault.go uses for a guard's verdict).
//
// A refusal the catalogue has no line for stays in the language it was written in, and a
// page whose shell ships no catalogue is English throughout: a page that declared
// Portuguese over English copy would be declaring a thing the page does not do.
func refusal(err error, loc *page.Locale) string {
	line := sentence(rest.Fault(err))
	key, known := refusalKey(err)
	if loc == nil || !known {
		return line
	}
	if said := loc.Text(key, line); said != line {
		return said
	}
	return line
}

// refusalKey names the copy one of this module's own refusals answers from. Only the
// sentinels the service declares are matched: a refusal this module did not name is not
// this module's to re-word, and the English the caller was given is the honest answer.
func refusalKey(err error) (string, bool) {
	switch {
	case errors.Is(err, contracts.ErrSelfReview):
		return "change.refusal.self_review", true
	case errors.Is(err, contracts.ErrStaleBase):
		return "change.refusal.stale_base", true
	case errors.Is(err, contracts.ErrUnsupportedSubject):
		return "change.refusal.unsupported_subject", true
	}
	return "", false
}

// refusalTitle is the alert's heading — the one word the page says about a command that
// failed before it reaches the sentence, which names itself.
func refusalTitle(loc *page.Locale) string {
	if loc == nil {
		return "That could not be done"
	}
	return loc.Text("change.refusal.title", "That could not be done")
}

// sentence is the refusal a person is shown. kit/problem keeps a 5xx's cause on the
// server's side of that line, so what reaches the alert is the detail the caller can act
// on and nothing that belongs to the server's log.
func sentence(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Detail
	}
	return err.Error()
}
