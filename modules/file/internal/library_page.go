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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// ThumbnailWidth is the width a grid tile asks the content door for. It is a member
// of contracts.VariantWidthSet rather than an invention: that set is the closed list
// the encode pass builds and the door accepts, so a tile asking for anything else
// would be a tile waiting on an object nobody wrote.
const ThumbnailWidth = 480

// widthQuery is the content door's own parameter, spelled as contentInput declares
// it (query:"w"). A tile that named the concept instead of the parameter would ask
// for the stored frame and be answered with it — the door ignores a query it does
// not declare — so a grid of 480-pixel tiles would fetch and decode every original.
const widthQuery = "?w="

// LibraryPages is the media library: what it reads, the two commands its controls
// post to, the shell it is drawn in, and whose answer to "may this caller write"
// the caption and removal controls reuse.
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

// libraryFaults are the statuses this page answers with beyond the shell's defaults,
// and every one is a status the service underneath already returns: no such file, a
// row the ledger still shows, a caption that contradicts itself, a row that moved
// past the one the person was shown, an unreadable database.
var libraryFaults = []int{
	http.StatusNotFound, http.StatusConflict,
	http.StatusUnprocessableEntity, http.StatusForbidden,
	http.StatusServiceUnavailable,
}

// MountLibrary serves the library and the two writes its controls post.
//
// The guard is the module's own permission on all three — file:read to look,
// file:manage to caption or remove — asked by the router before a line is rendered,
// so a person who may only look is given no form rather than a form that will refuse
// them. The posts are POST and not the JSON verb for the reason modules/change gives
// at its own decision forms: kit/httpx refuses an unsafe request carrying a session
// cookie unless it is same-site, and a <form> cannot send DELETE or PUT. The JSON
// doors stay exactly as they are for a client that can.
func MountLibrary(s httpx.Surfaces, p LibraryPages) {
	if p.Read == nil {
		return
	}
	where := s.App.PagePath("/files")
	bytes := s.App.Path("/files")
	read := httpx.Permission(contracts.PermissionFileRead)
	manage := httpx.Permission(contracts.PermissionFileManage)

	page.Serve(s.App, p.Shell, page.Route{ID: "file-library", Method: http.MethodGet,
		Path: "/files", Summary: "The media library", Errors: libraryFaults}, read,
		func(ctx context.Context, r page.Request, in *libraryInput) (page.View, error) {
			return p.render(ctx, r, where, bytes, in, nil)
		})

	page.Serve(s.App, p.Shell, page.Route{ID: "file-library-pick", Method: http.MethodGet,
		Path: "/files/pick", Summary: "Choose one of this tenant's images for a field",
		Errors: libraryFaults}, read,
		func(ctx context.Context, r page.Request, in *pickerInput) (page.View, error) {
			return p.pick(ctx, r, where, bytes, in)
		})

	page.Serve(s.App, p.Shell, page.Route{ID: "file-library-alt", Method: http.MethodPost,
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

	page.Serve(s.App, p.Shell, page.Route{ID: "file-library-delete", Method: http.MethodPost,
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
}

// libraryInput is the grid's whole query: the three filters, the order, which
// screenful, and which row is open in the panel. Every one is a query parameter a
// person can paste, reload and hand to a colleague, which is why none of them lives
// in a session or a script.
type libraryInput struct {
	Q      string `query:"q" maxLength:"64" doc:"Part of a file's name"`
	Type   string `query:"type" doc:"image, document or video"`
	Unused string `query:"unused" doc:"1 keeps the rows the use ledger stamped"`
	Sort   string `query:"sort" doc:"One of the six orders contracts.Sorts names"`
	Page   int    `query:"page" default:"1" minimum:"1" doc:"Which screenful"`
	File   string `query:"file" doc:"Open one file's details"`
}

// altForm is the caption post: which row, and the body the person posted. Like every
// hand-drawn form in the kernel it is read as url.Values by the handler rather than
// declared as a body schema, because a form written by hand posts exactly the fields
// the command underneath needs and no second vocabulary for them.
type altForm struct {
	ID      uuid.UUID `path:"id" format:"uuid" doc:"The file being captioned"`
	RawBody []byte    `contentType:"application/x-www-form-urlencoded"`
}

// idForm is the removal post, which carries nothing but the row it names.
type idForm struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The file to remove"`
}

// pickerInput is the picker's whole request: whose field is being filled, where the
// choice goes back to, and the grid's own filters over the eligible pictures.
type pickerInput struct {
	Field   string `query:"field" maxLength:"64" doc:"The calling field's own name"`
	Returns string `query:"returns" maxLength:"200" doc:"The workspace path the choice returns to"`
	Q       string `query:"q" maxLength:"64" doc:"Part of a file's name"`
	Sort    string `query:"sort" doc:"One of the six orders contracts.Sorts names"`
	Page    int    `query:"page" default:"1" minimum:"1" doc:"Which screenful"`
}

// oneFile is the grid query that opens exactly one row and filters nothing.
func oneFile(id uuid.UUID) *libraryInput {
	return &libraryInput{File: id.String()}
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
		body = append(body, components.Alert(components.AlertProps{
			Tone: "danger", Title: said(r, "file.library.refusal.title", "That could not be done"),
			Message: refusal(refused, r.Locale), Bordered: true,
		}))
	}
	body = append(body,
		components.Toolbar(components.ToolbarProps{
			Title: said(r, "file.library.title", "Media library"),
		}),
		uploadForm(r, bytes, may),
		meterPanel(r, meter, total),
		filterForm(r, where, in, query),
		grid(r, where, bytes, rows, total),
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

// grid is the tiles: one per row, never one per width, because a variant has no
// files row by decision and so has nothing to list, nothing to select and no second
// charge. Each tile links to this same page with itself open, so opening a picture's
// details needs no script, is linkable, and is reachable by a keyboard and by a flow
// that never saw this page before.
func grid(r page.Request, where, bytes string, rows []*contracts.File, total int64) g.Node {
	if total == 0 {
		// Nothing uploaded at all, said as one sentence rather than drawn as an empty
		// frame: the frame would promise a table with nothing in it.
		return components.EmptyState(components.EmptyStateProps{
			Title:    said(r, "file.library.empty.title", "Nothing is in the library yet"),
			Bordered: true,
		})
	}
	if len(rows) == 0 {
		// The filter matched nothing, which is a different fact from the tenant
		// holding nothing and needs a different sentence and a way out.
		return components.EmptyState(components.EmptyStateProps{
			Title: said(r, "file.library.noResults.title", "Nothing matches those filters"),
			Action: &components.ButtonProps{
				Label: said(r, "file.library.clear", "Clear filters"), Href: where, Variant: "secondary",
			},
			Bordered: true,
		})
	}
	photos := make([]components.Photo, 0, len(rows))
	for i, f := range rows {
		thumb, full := tile(r, bytes, f)
		label := fmt.Sprintf("%s · %s · %s", f.Name,
			contracts.MediaFamily(f.ContentType, f.Width), human(f.Size))
		if f.UnusedSince != nil {
			// A filter whose rows cannot show their own reason is a filter a person cannot
			// check, so the stamp the unused filter selected on travels on the tile.
			label += " · " + said(r, "file.library.unusedSince", "unused since") + " " + day(*f.UnusedSince)
		}
		photos = append(photos, components.Photo{
			ID:    f.ID.String(),
			Href:  where + "?file=" + f.ID.String(),
			Media: thumb, Full: full,
			PositionText: fmt.Sprintf("%s — %s %d of %d", label,
				said(r, "file.library.tile", "file"), i+1, total),
		})
	}
	// The name and the size are the line beneath every tile rather than a caption on
	// the picture: a document has no picture to caption, and a grid that names only
	// what it can draw would leave a person choosing between two tiles of identical
	// shape by their shape alone.
	return components.MasonryWithSlots(components.MasonryProps{
		Label: said(r, "file.library.grid", "Files in this tenant"),
		Items: photos,
	}, components.MasonrySlots{ItemActions: func(photo components.Photo) g.Node {
		return components.Text(components.TextProps{Content: photo.PositionText, Size: "sm", Color: "secondary"})
	}})
}

// tile is one row as a pair of pictures: the thumbnail at the width the grid asks
// for, and the frame itself for whoever opens it. A row no browser may be shown
// inline arrives as the empty media state saying so, because a broken-image glyph
// would be a claim about the file rather than about what this page can draw.
func tile(r page.Request, bytes string, f *contracts.File) (thumb, full components.MediaProps) {
	if contracts.MediaFamily(f.ContentType, f.Width) != contracts.FamilyImage || f.Height <= 0 {
		absent := said(r, "file.library.noPreview", "Nothing to preview here")
		return components.MediaProps{Status: components.MediaEmpty, Reason: absent},
			components.MediaProps{Status: components.MediaEmpty, Reason: absent}
	}
	// A picture nobody captioned yet is labelled by its name rather than by nothing:
	// the renderer refuses an empty alt on a picture nobody declared decorative, and
	// "IMG_2048.jpeg" is a description until somebody writes a better one.
	alt := f.Alt
	if f.Decorative {
		alt = "" // the honest empty alt: the person said this picture has nothing to say
	} else if alt == "" {
		alt = f.Name
	}
	return components.MediaProps{
		Src: bytes + "/" + f.ID.String() + "/content" + widthQuery + strconv.Itoa(ThumbnailWidth),
		Alt: alt, Decorative: f.Decorative, Width: ThumbnailWidth, Height: scaledHeight(f),
		Lazy: true,
	}, components.MediaProps{
		Src: bytes + "/" + f.ID.String() + "/content",
		Alt: alt, Decorative: f.Decorative, Width: f.Width, Height: f.Height, Fit: "contain",
	}
}

// scaledHeight is the tile's height at the width the tile asks for, from the frame
// the image pass measured — the pair that keeps the box the right shape until the
// bytes land. A row nobody measured carries no height, and the box is then the
// browser's to work out.
func scaledHeight(f *contracts.File) int {
	if f.Width <= 0 || f.Height <= 0 {
		return 0
	}
	return contracts.VariantHeight(f.Width, f.Height, ThumbnailWidth)
}

// filterForm is the grid's read as a plain GET form: search, family, order, the
// unused filter. It is written as a form rather than as links because a filter a
// person types is not a link anybody could compose, and it carries no script, so the
// filters work on a phone with no JavaScript, in a screen reader, and as a URL a
// person can send a colleague.
func filterForm(r page.Request, where string, in *libraryInput, query string) g.Node {
	options := make([]components.SelectOption, 0, len(contracts.Families))
	for _, f := range contracts.Families {
		options = append(options, components.SelectOption{Value: f, Label: familySaid(r, f)})
	}
	orders := make([]components.SelectOption, 0, len(contracts.Sorts))
	for _, s := range contracts.Sorts {
		orders = append(orders, components.SelectOption{Value: s, Label: orderSaidKey(r, s)})
	}
	nodes := []g.Node{
		h.Form(h.Method("get"), h.Action(where), g.Attr("aria-label", said(r, "file.library.title", "Media library")),
			components.Input(components.InputProps{
				Name: "q", Type: "search", Value: in.Q,
				Label: said(r, "file.library.filter.name", "Search by name"),
			}),
			components.Select(components.SelectProps{
				Name: "type", Label: said(r, "file.library.filter.type", "Type"),
				Value: orDefault(in.Type, "all"), Options: options,
			}),
			components.Select(components.SelectProps{
				Name: "sort", Label: said(r, "file.library.filter.sort", "Order by"),
				Value: orDefault(in.Sort, contracts.SortDefaultKey), Options: orders,
			}),
			components.Checkbox(components.CheckboxProps{
				Name: "unused", Value: "1", Checked: in.Unused != "",
				Label: said(r, "file.library.filter.unused", "Only what nothing shows"),
				HelpText: said(r, "file.library.filter.unusedHelp",
					"What the use ledger stamped — the files the release sweep will remove once the grace has run"),
			}),
			components.Button(components.ButtonProps{
				Type: "submit", Label: said(r, "file.library.filter.apply", "Apply"),
			}),
		),
	}
	if query != "" {
		// A second control, outside the form, because clearing is a link to the bare
		// page and not a submission of the fields above.
		nodes = append(nodes, components.Button(components.ButtonProps{
			Label: said(r, "file.library.clear", "Clear filters"), Href: where, Variant: "ghost",
		}))
	}
	return g.Group(nodes)
}

// meterPanel is the tenant's disk, drawn whether or not anything is running: the
// number the page shows is the number an upload is refused against — one sum, one
// reader, two callers, stated on contracts.Library.
//
// There is no bar, because the kit has no meter component and a page that drew its
// own would name a class nobody styles. Three numbers and a tone say the same thing,
// and the tone is the state a person can act on.
//
// The sentence beneath the numbers is written when there is a grid above it to sum up.
// With nothing held the numbers are the whole of what can be said, and a paragraph that
// restates "Held now: 0 B" is the library's third body size on the one page where it has
// nothing to add — the design floor permits two, and the other two belong to field help
// and to an empty region's title.
func meterPanel(r page.Request, q *contracts.Quota, shown int64) g.Node {
	items := []components.DetailItem{
		{Label: said(r, "file.library.meter.used", "Held now"), Value: human(q.Used)},
	}
	if q.Variants > 0 {
		items = append(items, components.DetailItem{
			Label: said(r, "file.library.meter.variants", "Of that, other widths"), Value: human(q.Variants),
		})
	}
	switch {
	case q.Unlimited:
		items = append(items, components.DetailItem{
			Label: said(r, "file.library.meter.ceiling", "Ceiling"),
			Value: said(r, "file.library.meter.none", "This installation sets none"),
		})
	default:
		tone := ""
		if q.Full() {
			tone = "danger"
		}
		items = append(items,
			components.DetailItem{Label: said(r, "file.library.meter.ceiling", "Ceiling"), Value: human(q.Limit)},
			components.DetailItem{
				Label: said(r, "file.library.meter.left", "Still fits"), Value: human(q.Left()), Tone: tone,
			},
		)
	}
	panel := components.DetailListProps{
		Title:        said(r, "file.library.meter.title", "Storage"),
		SemanticRole: "file-quota",
		Items:        items,
	}
	if shown > 0 {
		panel.Description = meterSaid(q)
	}
	return components.DetailList(panel)
}

// uploadForm is the library's one write of bytes: a plain multipart form whose action
// is this module's own upload door. The door streams the part to storage and answers
// with the row it wrote, and it is the only upload this module has — a page that posted
// to a second upload of its own would be a second implementation of a command that
// already refuses the too-large, the mislabelled and the full.
//
// The `image=true` is the door's own claim, not this page's guess about the file: it is
// what turns bytes no decoder reads into a refusal rather than an attachment, and a
// library whose grid is full of attachments nobody asked for is the failure it named.
func uploadForm(r page.Request, bytes string, may bool) g.Node {
	if !may {
		// A person who may not upload is shown no control, exactly as the caption and
		// removal forms work; the post would refuse them either way.
		return g.Text("")
	}
	return h.Form(h.Method("post"), h.Action(bytes+"?image=true"), h.EncType("multipart/form-data"),
		g.Attr("aria-label", said(r, "file.library.upload.label", "Add a file to the library")),
		components.Input(components.InputProps{
			Name: "file", Type: "file", Accept: "image/*",
			Label: said(r, "file.library.upload.pick", "Choose an image to add"),
			HelpText: said(r, "file.library.upload.help",
				"The bytes are counted against the storage above as they arrive, and an upload past what this deployment accepts is refused with nothing kept."),
		}),
		components.Button(components.ButtonProps{
			Type: "submit", Label: said(r, "file.library.upload.send", "Upload"), Variant: "primary",
		}),
	)
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

// pickerForm is the return contract as markup: one GET form whose action is the path the
// caller named, one radio per picture that may be chosen, and the caller's own field
// name as the parameter the answer arrives under. A person who selects and submits is
// answered by the page that asked, with the id in its query — no script between the two,
// and a browser with JavaScript off lands in exactly the same place.
//
// A row the module will not hand to a body — unmeasured, or with no words and no claim
// that it is decorative — carries no radio, because a control that will be refused on
// the called page is a control that lies. It says why instead, and links the panel that
// can fix it.
func pickerForm(r page.Request, where, bytes, field, back string, rows []*contracts.File) g.Node {
	if len(rows) == 0 {
		return components.EmptyState(components.EmptyStateProps{
			Title:       said(r, "file.library.pick.none.title", "No picture to choose"),
			Description: said(r, "file.library.pick.none", "This tenant holds no image this field could carry. Upload one from the library first."),
			Action: &components.ButtonProps{
				Label: said(r, "file.library.pick.back", "Back to the library"), Href: where, Variant: "secondary",
			},
			Bordered: true,
		})
	}
	tiles := []g.Node{}
	for _, f := range rows {
		_, preview := tile(r, bytes, f)
		if !contracts.Pickable(f) {
			tiles = append(tiles, h.Div(
				components.Media(preview),
				components.Text(components.TextProps{
					Content: said(r, "file.library.pick.refused", "This one cannot be chosen yet") + " — " +
						said(r, "file.library.pick.refusedWhy", "a picture has to say what is in it, or say that it is decorative"),
					Size: "sm", Color: "secondary",
				}),
				components.Button(components.ButtonProps{
					Label: said(r, "file.library.pick.panel", "Write its caption"),
					Href:  where + "?file=" + f.ID.String(), Variant: "ghost",
				}),
			))
			continue
		}
		tiles = append(tiles, h.Label(
			h.For("pk-picker-"+f.ID.String()),
			h.Input(h.Type("radio"), h.ID("pk-picker-"+f.ID.String()),
				h.Name(contracts.PickerParameter(field)), h.Value(f.ID.String())),
			components.Media(preview),
			components.Text(components.TextProps{Content: f.Name, Size: "sm"}),
		))
	}
	tiles = append(tiles, components.Button(components.ButtonProps{
		Type: "submit", Variant: "primary",
		Label: said(r, "file.library.pick.use", "Use image"),
	}))
	return h.Form(append([]g.Node{h.Method("get"), h.Action(back),
		g.Attr("aria-label", said(r, "file.library.pick.form", "Choose one of this tenant's images"))},
		tiles...)...)
}

// panel is one file: what it is, what shows it, what it says about itself, and the
// two controls a person who may write has. It is drawn server-side from ?file=, so
// opening it is a link, a URL and a keyboard destination rather than a script's side
// effect. A row the caller cannot see is a 404 and not an empty panel.
func (p LibraryPages) panel(ctx context.Context, r page.Request, where, bytes, asked string, may bool) (g.Node, error) {
	id, err := uuid.Parse(strings.TrimSpace(asked))
	if err != nil {
		// A panel nobody can name is not a refusal: the grid the person was looking at
		// is still the answer, and the id they pasted is not a thing this page made up.
		return g.Text(""), nil
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, storageUnreachable()
	}
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, err
	}
	uses, err := p.uses(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	body := []g.Node{components.DetailList(components.DetailListProps{
		Title:        f.Name,
		Description:  contracts.MediaFamily(f.ContentType, f.Width) + " · " + human(f.Size),
		SemanticRole: "file-library-row",
		Items:        facts(r, f, uses),
	})}
	if contracts.MediaFamily(f.ContentType, f.Width) == contracts.FamilyImage && f.Height > 0 {
		_, preview := tile(r, bytes, f)
		preview.Caption = said(r, "file.library.preview", "The frame as it is stored")
		body = append(body, components.Media(preview))
	}
	body = append(body, usesPanel(r, uses))
	if may {
		body = append(body, captionForm(ctx, r, where, f), removalForm(r, where, f, uses))
	}
	return components.SidePanelWithSlots(components.DetailPanelProps{
		ComponentProps: components.ComponentProps{ID: "file-library-panel-" + f.ID.String()},
		ItemID:         f.ID.String(), Title: f.Name,
		Description: said(r, "file.library.panel.about", "One file, as this tenant holds it"),
		Label:       said(r, "file.library.panel.label", "File details"),
		State:       components.ContentState{Status: components.MediaReady},
		CloseLabel:  said(r, "file.library.panel.close", "Close"),
		ReturnHref:  where,
	}, components.DetailPanelSlots{Body: body}), nil
}

// facts is the row as a person reads it. Every value is the row's own; nothing here
// is inferred, and no second number is drawn beside the one the API answers.
func facts(r page.Request, f *contracts.File, uses []contracts.UseRow) []components.DetailItem {
	out := []components.DetailItem{
		{Label: said(r, "file.library.fact.type", "Media type"), Value: f.ContentType},
		{Label: said(r, "file.library.fact.size", "Size"), Value: human(f.Size)},
		{Label: said(r, "file.library.fact.frame", "Frame"), Value: frame(r, f)},
		{Label: said(r, "file.library.fact.digest", "SHA-256"), Value: f.SHA256},
		{Label: said(r, "file.library.fact.visibility", "Visibility"), Value: f.Visibility},
		{Label: said(r, "file.library.fact.uploaded", "Uploaded"), Value: when(f.CreatedAt)},
		{Label: said(r, "file.library.fact.shown", "Shown by"), Value: strconv.Itoa(len(uses))},
	}
	if f.UnusedSince != nil {
		out = append(out, components.DetailItem{
			Label: said(r, "file.library.fact.unusedSince", "Unused since"), Value: when(*f.UnusedSince), Tone: "warning",
		})
	}
	if f.Kind != "" {
		out = append(out, components.DetailItem{
			Label: said(r, "file.library.fact.kind", "Retention class"), Value: f.Kind,
		})
	}
	return out
}

// usesPanel is the ledger: which record's field, in which locale, grouped by locale
// because a person looks for the Breton version rather than for the locale code. Each
// row is composed from the use row's own module and entity — the row already names the
// screen, and a page that switched on which module it was would be a page with another
// module's vocabulary in it.
func usesPanel(r page.Request, uses []contracts.UseRow) g.Node {
	if len(uses) == 0 {
		return components.DetailList(components.DetailListProps{
			Title:        said(r, "file.library.uses.title", "Used in"),
			Description:  said(r, "file.library.unused", "Nothing in this tenant shows it."),
			SemanticRole: "file-library-uses",
		})
	}
	byLocale := map[string][]contracts.UseRow{}
	for _, u := range uses {
		byLocale[u.Locale] = append(byLocale[u.Locale], u)
	}
	locales := make([]string, 0, len(byLocale))
	for locale := range byLocale {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	items := make([]components.DetailItem, 0, len(uses))
	for _, locale := range locales {
		for _, u := range byLocale[locale] {
			what := u.Entity + " " + u.Record.String()
			if u.Field != "" {
				what = u.Field + " of " + u.Entity + " " + u.Record.String()
			}
			items = append(items, components.DetailItem{
				Label: orDefault(locale, "—"), Value: u.Module + " · " + what,
			})
		}
	}
	return components.DetailList(components.DetailListProps{
		Title: fmt.Sprintf("%s", said(r, "file.library.uses.title", "Used in")),
		Description: fmt.Sprintf("%d %s", len(uses),
			plural(int64(len(uses)), "record shows it", "records show it")),
		SemanticRole: "file-library-uses",
		Items:        items,
	})
}

// captionForm is the one write a picture's words have: a real form, in reading order,
// carrying the UpdatedAt the panel was drawn from so the command underneath can refuse
// a caption typed over an edit the writer never saw.
//
// The bound is the entity's own — contracts.MaxAlt, the column's own length — and the
// checkbox is the alternative to the textarea rather than a second claim on top of it:
// contracts.AltClaim refuses both at once and neither, in one sentence this page passes
// through rather than re-wording.
//
// Which of the two the person is making is decided by the checkbox, so `required` belongs
// to the checkbox's state and to nothing else: a textarea marked required because the row
// was not decorative when it was drawn would refuse the caption that tick says should be
// written, and the form would never reach the command that answers it. The snippet below
// keeps the two in step from load; without a script the rule is still kept, one submit
// later, by AltClaim's refusal on the page that carried the form.
func captionForm(ctx context.Context, r page.Request, where string, f *contracts.File) g.Node {
	return h.Form(h.Method("post"), h.Action(where+"/"+f.ID.String()+"/alt"),
		g.Attr("aria-label", said(r, "file.library.caption.label", "What this image says about itself")),
		g.Attr("data-file-caption", ""),
		components.Textarea(components.TextareaProps{
			Name: "alt", Value: f.Alt, Rows: 3, MaxLength: contracts.MaxAlt,
			Label: said(r, "file.library.caption.label", "What this image says about itself"),
			HelperText: said(r, "file.library.altRequired",
				"An image either says what is in it or says that it is decorative."),
			Required: !f.Decorative,
		}),
		components.Checkbox(components.CheckboxProps{
			Name: "decorative", Checked: f.Decorative,
			Label: said(r, "file.library.decorative", "Decorative image"),
			HelpText: said(r, "file.library.decorativeHelp",
				"Checked, the image says nothing about itself and no text is asked for."),
		}),
		h.Input(h.Type("hidden"), h.Name("updated"), h.Value(f.UpdatedAt.UTC().Format(time.RFC3339Nano))),
		components.Button(components.ButtonProps{
			Type: "submit", Variant: "primary",
			Label: said(r, "file.library.caption.save", "Save caption"),
		}),
		page.InlineScript(ctx, captionToggle),
	)
}

// captionToggle is the form's one piece of client-side behaviour: the decorative claim
// and the required caption are the same decision, and the checkbox is what says it. It
// asks for the two controls by their component names inside this form alone, and it is
// carried with the response's content-security-policy nonce, which ui/page renders and
// nothing else in a module can reach.
const captionToggle = `(()=>{const f=document.currentScript.closest("form");if(!f)return;const d=f.querySelector("[name=decorative]"),a=f.querySelector("[name=alt]");if(!d||!a)return;const sync=()=>{a.required=!d.checked;a.setAttribute("aria-required",String(!d.checked));if(d.checked)a.value=""};d.addEventListener("change",sync);sync()})()`

// removalForm is the delete — a form rather than the JSON verb for the reason the
// module's own posts give. It is disabled while the ledger shows the file, with the
// reason in the control's own help text, and the command refuses the same thing
// whatever the button looks like: how the button looks is a convenience, and the check
// that decides is inside the transaction.
func removalForm(r page.Request, where string, f *contracts.File, uses []contracts.UseRow) g.Node {
	shown := len(uses) > 0
	help := said(r, "file.library.deleteHelp", "The bytes go once the transaction that removed the row has committed.")
	if shown {
		help = said(r, "file.library.inUse",
			"Something still shows this file, so it cannot be removed; that record has to stop showing it first.")
	}
	return h.Form(h.Method("post"), h.Action(where+"/"+f.ID.String()+"/delete"),
		g.Attr("aria-label", said(r, "file.library.delete", "Remove this file")),
		components.FormSentence(components.FormSentenceProps{Text: help}),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Disabled: shown},
			Type:           "submit", Label: said(r, "file.library.delete", "Remove this file"),
			Tone: "danger", Variant: "outline",
		}),
	)
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

// meterSaid is the one sentence the meter and the toolbar share: what is held,
// against what, in words a person who is not reading a struct can act on.
func meterSaid(q *contracts.Quota) string {
	if q.Unlimited {
		return fmt.Sprintf("%s held, with no ceiling set.", human(q.Used))
	}
	if q.Full() {
		return fmt.Sprintf("%s of %s held — nothing more fits.", human(q.Used), human(q.Limit))
	}
	return fmt.Sprintf("%s of %s held.", human(q.Used), human(q.Limit))
}

// familySaid is one filter option in the language of the request.
func familySaid(r page.Request, family string) string {
	switch family {
	case "all":
		return said(r, "file.library.family.all", "Every type")
	case contracts.FamilyImage:
		return said(r, "file.library.family.image", "Images")
	case contracts.FamilyDocument:
		return said(r, "file.library.family.document", "Documents")
	case contracts.FamilyVideo:
		return said(r, "file.library.family.video", "Videos")
	}
	return family
}

// orderSaidKey is one sort option in the language of the request.
func orderSaidKey(r page.Request, spec string) string {
	switch spec {
	case contracts.SortNewest:
		return said(r, "file.library.sort.newest", "Newest first")
	case contracts.SortOldest:
		return said(r, "file.library.sort.oldest", "Oldest first")
	case contracts.SortName:
		return said(r, "file.library.sort.name", "Name, A to Z")
	case contracts.SortNameDesc:
		return said(r, "file.library.sort.nameDesc", "Name, Z to A")
	case contracts.SortSize:
		return said(r, "file.library.sort.size", "Smallest first")
	case contracts.SortSizeDesc:
		return said(r, "file.library.sort.sizeDesc", "Largest first")
	}
	return spec
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

// formInstant is the timestamp the caption form carried back — the row's own
// UpdatedAt, formatted the way the panel wrote it. A form that carried nothing asks
// for a write against nothing, which SetAlt refuses rather than guessing at one.
func formInstant(text string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil {
		return time.Time{}
	}
	return at.UTC()
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
