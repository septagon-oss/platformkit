package internal

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// grid is the tiles: one per row, never one per width, because a variant has no
// files row by decision and so has nothing to list, nothing to select and no second
// charge. Each tile links to this same page with itself open, so opening a picture's
// details needs no script, is linkable, and is reachable by a keyboard and by a flow
// that never saw this page before.
func grid(r page.Request, where, bytes string, rows []*contracts.File, total int64, ready map[uuid.UUID][]int) g.Node {
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
		thumb, full := tile(r, bytes, f, ready[f.ID])
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

// tile is one row as a pair of pictures: the thumbnail at the widest built width the
// grid can use, and the frame itself for whoever opens it. A row no browser may be
// shown inline arrives as the empty media state saying so, because a broken-image glyph
// would be a claim about the file rather than about what this page can draw.
func tile(r page.Request, bytes string, f *contracts.File, built []int) (thumb, full components.MediaProps) {
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
	src, width, height := thumbnail(bytes, f, built)
	return components.MediaProps{
		Src: src, Alt: alt, Decorative: f.Decorative, Width: width, Height: height,
		Lazy: true,
	}, components.MediaProps{
		Src: bytes + "/" + f.ID.String() + "/content",
		Alt: alt, Decorative: f.Decorative, Width: f.Width, Height: f.Height, Fit: "contain",
	}
}

// thumbnail is the address a tile names and the box it names it in: the widest of the
// row's ready widths that still fits the tile, or the frame's own address when the row
// has none.
//
// The frame is not a compromise, it is what the row has. A picture no wider than the
// tile never acquires a width — contracts.VariantWidths plans strictly smaller widths
// and never upscales — and a picture whose encode pass has not run has nothing ready
// yet. Naming the tile's width in either case names an object nobody wrote, and the
// grid answers that with the door's 404 behind a broken-image glyph.
func thumbnail(bytes string, f *contracts.File, built []int) (string, int, int) {
	at := bytes + "/" + f.ID.String() + "/content"
	widest := 0
	for _, w := range built {
		if w <= ThumbnailWidth && w > widest {
			widest = w
		}
	}
	if widest == 0 {
		return at, f.Width, f.Height
	}
	return at + widthQuery + strconv.Itoa(widest), widest, contracts.VariantHeight(f.Width, f.Height, widest)
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

// refusedLine is a command the page refused, as the page says it: the fact that
// nothing was written, and the sentence that says why.
//
// It is not components.Alert, and the reason is the design floor rather than taste.
// The alert sets its message beside an icon and inside its own padding, sixty-eight
// pixels in from the content column, and this page has exactly the two alignments the
// floor allows already — the grid, meter and filters on the page's own column, and the
// panel on the rail it opens over. A refusal that took a third alignment would be the
// one line on the page that does not stand where the page's copy stands, and the page
// would refuse a caption by moving the sentence that says so.
//
// What the alert owns and this line keeps is the announcement: role and aria-live go
// on the text itself, so the sentence is read out when it lands rather than being
// something the person has to look for, and the danger foreground is the one the
// form's own field errors wear.
func refusedLine(r page.Request, refused error) g.Node {
	return components.Text(components.TextProps{
		ComponentProps: components.ComponentProps{Attrs: map[string]string{
			"role": "alert", "aria-live": "assertive", "aria-atomic": "true",
		}},
		Content: said(r, "file.library.refusal.title", "That could not be done") + ". " +
			refusal(refused, r.Locale),
		Size: "sm", Color: "danger",
	})
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
		_, preview := tile(r, bytes, f, nil)
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
