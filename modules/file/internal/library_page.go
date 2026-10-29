package internal

// The media library's own screen: the grid a tenant's files are read through, the
// meter that says how full its disk is, and the panel one file opens in. It lives
// in this package beside the service it reads for the reason modules/change's two
// pages live in its own internal: one package per module's implementations, and the
// page layer is the foundation's (ui/page, ui/components), which this file uses and
// does not become. It is hand-drawn rather than generated because the library is not
// a resource a rest.Spec describes: a selection, a use ledger, a caption with a
// revision guard on it, and a meter that is a refusal's own number.
//
// What this module owns is this page and the two controls on it. What it does not
// own is the shell they are drawn in, which the composition hands in as the same
// page.Shell its fault pages use, or the answer to "may this caller write", which
// arrives as the composition's own httpx.Authorizer so the caption form is drawn for
// the people who can press it and not hidden from the others.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// ThumbnailWidth is the widest picture a grid tile asks the content door for. It is a
// member of contracts.VariantWidthSet rather than an invention: that set is the closed
// list the encode pass builds and the door accepts, so a tile asking for anything else
// would be a tile waiting on an object nobody wrote.
//
// It is a ceiling and not an order. What a tile may name is a width this row has
// actually got — see tile — and a frame smaller than this, or one the encode pass has
// not reached, is drawn from the frame's own address.
const ThumbnailWidth = 480

// widthQuery is the content door's own parameter, spelled as contentInput declares
// it (query:"w"). A tile that named the concept instead of the parameter would ask
// for the stored frame and be answered with it — the door ignores a query it does
// not declare — so a grid of 480-pixel tiles would fetch and decode every original.
const widthQuery = "?w="

// LibraryPages is the media library: what it reads, the three commands its
// controls post to, the shell it is drawn in, and whose answer to "may this
// caller write" the caption, removal and upload controls reuse.
//
// A nil Read mounts nothing at all: a composition that wired this module's doors and
// no library is an ordinary installation, and the honest answer to a person who asks
// for a screen nobody composed is the surface's own 404.
//
// A nil Authorize draws the two write controls for anybody who reached the page. The
// guard on the post is what decides; a page that drew no controls because nobody had
// wired the question would be a page that refuses its own subject.
type LibraryPages struct {
	Shell     page.Shell
	Read      contracts.Library
	Shown     UseReader
	Remove    Remover
	Add       Storer
	Widths    WidthReader
	Authorize httpx.Authorizer
	PerPage   int
}

// UseReader is the panel's question — what shows this file — and nothing else. The
// page takes the narrow read rather than the whole Service because the controls it
// draws are decided by the ledger and by no other command.
type UseReader interface {
	Uses(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]contracts.UseRow, error)
}

// Remover is the one command the page's Remove control posts to. contracts.Service has
// it; the page names it separately for the reason UseReader does.
type Remover interface {
	Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.File, error)
}

// Storer is the one command the page's upload control posts to, and the same one the
// JSON upload door performs: one command, one guard, two faces of it, so the meter on
// the page and the refusal an upload gets cannot be two sums.
type Storer interface {
	Upload(ctx context.Context, tx contracts.Tx, up contracts.Upload) (*contracts.File, error)
}

// WidthReader is the grid's other question: of the widths this module can serve, which
// ones does each of these rows have standing ready. It is asked once for the page,
// because a tile that asked per row would be one query per picture on the screen.
//
// The page asks because a tile is a promise about a picture: an address it names has
// to answer when the browser fetches it, and a width is only an object once the encode
// pass has written it. A composition that wires no reader draws every tile from the
// frame, which is the one address that always serves.
type WidthReader interface {
	ReadyWidths(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID) (map[uuid.UUID][]int, error)
}

// libraryFaults are the statuses this page answers with beyond the shell's defaults,
// and every one is a status the service underneath already returns: no such file, a
// row the ledger still shows, a caption that contradicts itself, a row that moved
// past the one the person was shown, an unreadable database.
var libraryFaults = []int{
	http.StatusNotFound, http.StatusConflict,
	http.StatusUnprocessableEntity, http.StatusForbidden,
	http.StatusServiceUnavailable,
}

// uploadFaults are those plus the one status no other control on the page can answer
// with: bytes past what this deployment accepts, which is the refusal an upload is
// refused by and the only one a person can act on by sending something smaller.
var uploadFaults = append(slices.Clone(libraryFaults), http.StatusRequestEntityTooLarge)

// MountLibrary serves the library and the three writes its controls post.
//
// The guard is the module's own permission on all four — file:read to look,
// file:manage to caption, remove or add — asked by the router before a line is
// rendered, so a person who may only look is given no form rather than a form that
// will refuse them. The posts are POST to this page's own routes and not the JSON
// verbs, for the reason modules/change gives at its own decision forms: kit/httpx
// refuses an unsafe request carrying a session cookie unless it is same-site, a
// <form> cannot send DELETE or PUT, and a person who submits a form from a page has
// to land on a page. The JSON doors stay exactly as they are for a client that can
// read the answer.
func MountLibrary(s httpx.Surfaces, p LibraryPages) {
	if p.Read == nil {
		return
	}
	where := s.App.PagePath("/files")
	bytes := s.App.Path("/files")
	read := httpx.Permission(contracts.PermissionFileRead)
	manage := httpx.Permission(contracts.PermissionFileManage)

	page.Serve(s.App, p.Shell, page.Route{View: contracts.Screen("library"), ID: "file-library", Method: http.MethodGet,
		Path: "/files", Summary: "The media library", Errors: libraryFaults}, read,
		func(ctx context.Context, r page.Request, in *libraryInput) (page.View, error) {
			return p.render(ctx, r, where, bytes, in, nil)
		})

	page.Serve(s.App, p.Shell, page.Route{View: contracts.Screen("pick"), ID: "file-library-pick", Method: http.MethodGet,
		Path: "/files/pick", Summary: "Choose one of this tenant's images for a field",
		Errors: libraryFaults}, read,
		func(ctx context.Context, r page.Request, in *pickerInput) (page.View, error) {
			return p.pick(ctx, r, where, bytes, in)
		})

	page.Serve(s.App, p.Shell, page.Route{View: contracts.Screen("library"), ID: "file-library-alt", Method: http.MethodPost,
		Path: "/files/{id}/alt", Summary: "Write what a picture says about itself",
		Errors: libraryFaults}, manage,
		func(ctx context.Context, r page.Request, in *altForm) (page.View, error) {
			sent, err := url.ParseQuery(string(in.RawBody))
			if err != nil {
				return p.render(ctx, r, where, bytes, oneFile(in.ID), problem.New(http.StatusUnprocessableEntity,
					said(r, "file.library.refusal.form", "The form could not be read, so nothing was written.")))
			}
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return page.View{}, storageUnreachable()
			}
			f, err := p.Read.SetAlt(ctx, tx, in.ID, sent.Get("alt"),
				sent.Get("decorative") == "on", formInstant(sent.Get("updated")))
			if err != nil {
				return p.render(ctx, r, where, bytes, oneFile(in.ID), err)
			}
			// SeeOther, so a reload cannot write the caption twice, and back to the
			// panel that now shows the row the command returned.
			return page.View{}, httpx.SeeOther(where + "?file=" + f.ID.String())
		})

	page.Serve(s.App, p.Shell, page.Route{View: contracts.Screen("library"), ID: "file-library-delete", Method: http.MethodPost,
		Path: "/files/{id}/delete", Summary: "Remove a file nothing shows",
		Errors: libraryFaults}, manage,
		func(ctx context.Context, r page.Request, in *idForm) (page.View, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return page.View{}, storageUnreachable()
			}
			if p.Remove == nil {
				// No command wired. The page drew no control, so whatever reached this
				// door was not the page's own form.
				return page.View{}, problem.NotFound("this installation offers no removal control")
			}
			if _, err := p.Remove.Delete(ctx, tx, in.ID); err != nil {
				return p.render(ctx, r, where, bytes, oneFile(in.ID), err)
			}
			// Back to the grid, not to the row that is gone.
			return page.View{}, httpx.SeeOther(where)
		})

	// The upload. It is a page route and not the JSON door because a form a person
	// submits from the library has to land them back on the library: the JSON door
	// answers with the row it wrote, which is the right answer for a program reading
	// it and a raw document to a person who pressed Upload. The bytes are the same
	// command either way — the streamed multipart the JSON door reads, read off this
	// request and given to the same service — so the ceiling, the counting and the
	// refusal are one rule and not two. Refusals come back as the library page with
	// the sentence above the grid, because the grid is where the person was.
	page.Serve(s.App, p.Shell, page.Route{View: contracts.Screen("library"), ID: "file-library-upload", Method: http.MethodPost,
		Path: "/files/upload", Summary: "Add a file to the library",
		Errors: uploadFaults, StreamsBody: true}, manage,
		func(ctx context.Context, r page.Request, in *uploadQuery) (page.View, error) {
			if p.Add == nil {
				return page.View{}, problem.NotFound("this installation offers no upload control")
			}
			up, err := arriving(ctx)
			if err != nil {
				return p.render(ctx, r, where, bytes, &libraryInput{}, err)
			}
			up.Image = in.Image
			f, err := p.Add.Upload(ctx, transaction, up)
			if err != nil {
				return p.render(ctx, r, where, bytes, &libraryInput{}, err)
			}
			// SeeOther, so a reload cannot store the same file twice, and to the panel of
			// the file that arrived: the one place that says both that it is here and
			// that it has no caption yet.
			return page.View{}, httpx.SeeOther(where + "?file=" + f.ID.String())
		})
}

// render is the library: the grid, the meter and — when the query names one — the
// panel. It takes the whole query rather than the row it is showing because a refused
// command re-renders the page the person was on, filters and all, with the refusal
// above it: a page that dropped them onto a bare grid after saying "something still
// shows this file" would be a page that had lost their place.
//
// refused is the command's own error, or nil. Its status is the page's, so a reload of
// the answer says what the click did, and its sentence is the one the catalogue
// translates. Anything else that fails is a refusal of *this* read and leaves as a
// problem of its own status: no such row is a 404 and a filter nobody can correct is a
// 422, not the 500 a raw crud error would be in the shell.
func (p LibraryPages) render(ctx context.Context, r page.Request, where, bytes string,
	in *libraryInput, refused error) (page.View, error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return page.View{}, storageUnreachable()
	}
	per := p.perPage()
	at := max(in.Page, 1)
	rows, total, err := p.Read.Search(ctx, tx, contracts.LibraryQuery{
		Name: in.Q, Family: in.Type, Unused: in.Unused != "", Sort: in.Sort,
		Limit: per, Offset: (at - 1) * per,
	})
	if err != nil {
		return page.View{}, fault(err)
	}
	meter, err := p.Read.Quota(ctx, tx)
	if err != nil {
		return page.View{}, fault(err)
	}
	// Which of the grid's rows have a width standing ready, asked once for the page.
	ready, err := p.readyWidths(ctx, tx, rows)
	if err != nil {
		return page.View{}, fault(err)
	}
	may, err := p.mayManage(ctx, r)
	if err != nil {
		return page.View{}, fault(err)
	}
	query := filters(in)
	last := max(int((total+int64(per)-1)/int64(per)), 1)
	body := []g.Node{}
	status := 0
	if refused != nil {
		// The status of a page that refused you is the status of the refusal, so a
		// reload of the answer says what the click did. An answer with no status a
		// caller can act on is not a refusal of the person at the keyboard, and it
		// leaves as an error instead of as an alert that quotes the server's log.
		status = refusalStatus(refused)
		if status >= http.StatusInternalServerError {
			return page.View{}, refused
		}
		body = append(body, refusedLine(r, refused))
	}
	body = append(body,
		components.Toolbar(components.ToolbarProps{
			Title: said(r, "file.library.title", "Media library"),
		}),
		uploadForm(r, where, may),
		meterPanel(r, meter, total),
		filterForm(r, where, in, query),
		grid(r, where, bytes, rows, total, ready),
		components.Pagination(components.PaginationProps{
			CurrentPage: min(at, last), TotalPages: last, BaseURL: where + "?" + query,
			NavigationLabel: said(r, "file.library.pagination", "Media library pagination"),
		}),
	)
	if in.File != "" {
		panel, err := p.panel(ctx, r, where, bytes, in.File, may)
		if err != nil {
			return page.View{}, fault(err)
		}
		body = append(body, panel)
	}
	return page.View{
		Title: said(r, "file.library.title", "Media library"),
		Body:  body, Status: status, Sensitive: true,
	}, nil
}

// readyWidths is the page's one ask about widths, answered as nothing when the
// composition wired no reader: every tile then draws the frame, which is the honest
// fallback rather than a guess at what the encode pass got round to.
func (p LibraryPages) readyWidths(ctx context.Context, tx db.Tx[db.Tenant], rows []*contracts.File) (map[uuid.UUID][]int, error) {
	if p.Widths == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, f := range rows {
		ids = append(ids, f.ID)
	}
	return p.Widths.ReadyWidths(ctx, tx, ids)
}

// pick is the picker: the same grid, narrowed to the pictures a field may take, in a
// form that navigates back to whoever opened it. It writes nothing — no session key,
// no row, no event — because a selection a person never saved is not a fact about any
// record, and a page that stored one would be a page whose later navigation rewrote
// somebody's draft.
func (p LibraryPages) pick(ctx context.Context, r page.Request, where, bytes string, in *pickerInput) (page.View, error) {
	field, err := contracts.PickerField(in.Field)
	if err != nil {
		return page.View{}, fault(err)
	}
	back, err := contracts.PickerReturns(in.Returns)
	if err != nil {
		return page.View{}, fault(err)
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return page.View{}, storageUnreachable()
	}
	at := max(in.Page, 1)
	rows, total, err := p.Read.Search(ctx, tx, contracts.LibraryQuery{
		Name: in.Q, Family: contracts.FamilyImage, Sort: in.Sort,
		Limit: p.perPage(), Offset: (at - 1) * p.perPage(),
	})
	if err != nil {
		return page.View{}, fault(err)
	}
	per := p.perPage()
	last := max(int((total+int64(per)-1)/int64(per)), 1)
	query := url.Values{}
	if in.Q != "" {
		query.Set("q", in.Q)
	}
	if in.Sort != "" {
		query.Set("sort", in.Sort)
	}
	query.Set("field", field)
	query.Set("returns", back)
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: said(r, "file.library.pick.title", "Choose an image"),
		}),
		pickerForm(r, where, bytes, field, back, rows),
		components.Pagination(components.PaginationProps{
			CurrentPage: min(at, last), TotalPages: last, BaseURL: where + "/pick?" + query.Encode(),
			NavigationLabel: said(r, "file.library.pick.pagination", "Image picker pagination"),
		}),
	}
	return page.View{
		Title: said(r, "file.library.pick.title", "Choose an image"),
		Body:  body, Sensitive: true,
	}, nil
}

// uses answers the panel's question, and answers it as nothing when the composition
// wired no ledger: a page that said "nothing shows it" on the strength of a missing
// wire would be a page that lied about somebody's picture.
func (p LibraryPages) uses(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) ([]contracts.UseRow, error) {
	if p.Shown == nil {
		return nil, nil
	}
	return p.Shown.Uses(ctx, tx, id)
}

// mayManage asks the composition's own authorizer, which is the one that enforces
// every route here. A page with its own rules about who looks like an editor is a page
// that lies in one direction or the other.
func (p LibraryPages) mayManage(ctx context.Context, r page.Request) (bool, error) {
	if p.Authorize == nil {
		return r.SignedIn, nil
	}
	ok, err := p.Authorize.Allowed(ctx, r.Tenant, tenancy.Grant{Permission: contracts.PermissionFileManage})
	if err != nil {
		// The question failed, so the controls cannot be drawn truthfully either way.
		return false, err
	}
	return ok, nil
}

// perPage is the screenful, defaulted once to the number the generated lists draw.
func (p LibraryPages) perPage() int {
	if p.PerPage > 0 {
		return p.PerPage
	}
	return contracts.DefaultLibraryPageSize
}

// filters is the query as a link, without the page number and without the open panel,
// so that clearing, the pagination and the tile links all keep the filters a person
// set without any of them rebuilding the form.
func filters(in *libraryInput) string {
	set := url.Values{}
	if in.Q != "" {
		set.Set("q", in.Q)
	}
	if in.Type != "" && in.Type != "all" {
		set.Set("type", in.Type)
	}
	if in.Unused != "" {
		set.Set("unused", "1")
	}
	if in.Sort != "" {
		set.Set("sort", in.Sort)
	}
	return set.Encode()
}

// frame is the measured frame, or the sentence that says nobody measured: zero is
// never a real width, and a panel printing "0 × 0" would be printing a number the row
// does not hold.
func frame(r page.Request, f *contracts.File) string {
	if f.Width <= 0 || f.Height <= 0 {
		return said(r, "file.library.fact.unmeasured", "not measured")
	}
	return fmt.Sprintf("%d × %d", f.Width, f.Height)
}

// refusalStatus is the status a refused command leaves on the page it re-rendered: a
// problem's own, or kit/rest's reading of the sentinel underneath it. Those two steps are
// the ones `fault` takes behind the JSON door, so a caption refused at the page and the
// same caption refused at PUT answer with one status and not two.
func refusalStatus(err error) int {
	var p *problem.Problem
	if errors.As(fault(err), &p) {
		return p.Status
	}
	return http.StatusInternalServerError
}

// refusal is the sentence a refused command leaves on the page, in the language the
// request was answered in. This module owns these refusals, so it owns their copy: the
// English is the service's own line, passed as the readable fallback, and the catalogue
// in modules/file/messages answers the key in another language (decision 0012 rule 2 —
// the shape modules/change's queue page uses).
//
// A refusal the catalogue has no line for stays in the language it was written in, and
// a page whose shell ships no catalogue is English throughout.
func refusal(err error, loc *page.Locale) string {
	line := problemDetail(err)
	key, known := refusalKey(err)
	if loc == nil || !known {
		return line
	}
	if said := loc.Text(key, line); said != line {
		return said
	}
	return line
}

// refusalKey names the copy one of this module's own refusals answers from. Only a
// sentinel the service declares, or the generic conflict and validation it refuses
// with, are matched: a refusal this module did not name is not this module's to
// re-word, so the caller keeps the sentence they were given.
func refusalKey(err error) (string, bool) {
	switch {
	case errors.Is(err, contracts.ErrInUse):
		return "file.library.refusal.in_use", true
	case errors.Is(err, contracts.ErrHeld):
		return "file.library.refusal.held", true
	case errors.Is(err, crud.ErrConflict):
		return "file.library.stale", true
	case errors.Is(err, crud.ErrInvalid):
		return "file.library.refusal.invalid", true
	case errors.Is(err, crud.ErrNotFound):
		return "file.library.refusal.not_found", true
	}
	return "", false
}

// problemDetail is the line a person is shown. kit/problem keeps a 5xx's cause on the
// server's side of that line, so what reaches the alert is the detail a caller can act
// on and nothing that belongs to the server's log.
func problemDetail(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Detail
	}
	return err.Error()
}

// said is one piece of the page's copy, through the request's catalogue or, when the
// shell ships none, in the language it was written in.
func said(r page.Request, key, text string) string {
	if r.Locale == nil {
		return text
	}
	return r.Locale.Text(key, text)
}

// storageUnreachable is the page's answer when the request carried no transaction: the
// same 503 the module's own doors answer with, and no cause across the line.
func storageUnreachable() error {
	return problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
}

// human is a byte count as a person reads it. The grid says it on every tile and the
// meter says it twice, so it is decided once here rather than per caller.
func human(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		value /= unit
		if value < unit || u == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

// day is the date a tile's badge prints without a clock's noise: the unused filter
// says "since", and a person checks that against a calendar rather than a nanosecond.
func day(at time.Time) string {
	return at.UTC().Format("2 Jan 2006")
}

// when is a timestamp the way the page reads it, in UTC, which is the only instant a
// row holds.
func when(at time.Time) string {
	return at.UTC().Format(time.RFC3339)
}
