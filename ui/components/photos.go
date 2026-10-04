package components

import (
	"fmt"
	"strconv"

	"github.com/septagon-oss/platformkit/ui/style"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type Photo struct {
	ID           string     `json:"id"`
	Media        MediaProps `json:"media"`
	Full         MediaProps `json:"full"`
	Href         string     `json:"href"`
	PositionText string     `json:"positionText"`
}
type PhotoGalleryProps struct {
	ComponentProps
	Label         string       `json:"label"`
	State         ContentState `json:"state,omitzero"`
	Items         []Photo      `json:"items,omitempty"`
	SelectedID    string       `json:"selectedID,omitempty"`
	ViewerOpen    bool         `json:"viewerOpen,omitzero"`
	PreviousLabel string       `json:"previousLabel"`
	NextLabel     string       `json:"nextLabel"`
	CloseLabel    string       `json:"closeLabel"`
	ReturnHref    string       `json:"returnHref"`
}
type MasonryProps struct {
	ComponentProps
	Label     string       `json:"label"`
	State     ContentState `json:"state,omitzero"`
	Items     []Photo      `json:"items,omitempty"`
	Columns   int          `json:"columns,omitzero" enum:"0,1,2,3,4"`
	SMColumns int          `json:"smColumns,omitzero" enum:"0,1,2,3,4"`
	LGColumns int          `json:"lgColumns,omitzero" enum:"0,1,2,3,4"`
	Gap       string       `json:"gap,omitempty"`
	More      *ChoiceLink  `json:"more,omitempty"`
}
type MasonrySlots struct {
	StateSlots
	ItemActions func(Photo) g.Node
}

// photoThumb wraps one collection item's media in its link. A disabled collection
// keeps its items readable but offers no destination to follow: an anchor with no
// href is inert to pointer, keyboard and enhancement alike, which is the shape a
// disabled navigation choice takes everywhere else.
func photoThumb(disabled bool, href string, media ...g.Node) g.Node {
	if disabled {
		return h.A(append([]g.Node{h.Role("link"), g.Attr("aria-disabled", "true"), g.Attr("tabindex", "-1")}, media...)...)
	}
	return h.A(append([]g.Node{h.Href(href)}, media...)...)
}

func validatePhotoMedia(p MediaProps) error {
	switch p.Status {
	case "", MediaReady:
		if !required(p.Src) || p.Width <= 0 || p.Height <= 0 || p.Decorative && p.Alt != "" || !p.Decorative && !required(p.Alt) {
			return fmt.Errorf("Photo: ready images require dimensions and meaningful alt or explicit decoration")
		}
	case MediaLoading:
		if p.Src != "" || p.Width <= 0 || p.Height <= 0 || !required(p.Alt) {
			return fmt.Errorf("Photo: loading requires cleared source, dimensions and loading copy")
		}
	case MediaEmpty, MediaFailed, MediaRefused:
		if p.Src != "" || !required(p.Reason) || p.Status == MediaRefused && (p.Alt != "" || p.Caption != "") {
			return fmt.Errorf("Photo: absent image must clear source and explain its state")
		}
	default:
		return fmt.Errorf("Photo: unknown media state")
	}
	return nil
}
func validatePhotos(items []Photo) error {
	ids := map[string]bool{}
	for _, item := range items {
		if !required(item.ID, item.Href, item.PositionText) || ids[item.ID] {
			return fmt.Errorf("Photo: unique IDs, URLs and position labels required")
		}
		ids[item.ID] = true
		if err := validatePhotoMedia(item.Media); err != nil {
			return err
		}
		if err := validatePhotoMedia(item.Full); err != nil {
			return err
		}
		if (item.Media.Status == MediaFailed || item.Media.Status == MediaRefused) && (item.Full.Src != "" || item.Media.Caption != "" || item.Full.Caption != "") {
			return fmt.Errorf("Photo: unavailable thumbnail must clear full source and captions")
		}
	}
	return nil
}
func photoMedia(p MediaProps) g.Node {
	attrs := []g.Node{g.Attr("data-shared-photo", "")}
	if p.Width > 0 && p.Height > 0 {
		attrs = append(attrs, g.Attr("style", fmt.Sprintf("aspect-ratio:%d/%d", p.Width, p.Height)))
	}
	return h.Div(append(attrs, Media(p))...)
}
func (p PhotoGalleryProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Items) > 0 || p.SelectedID != "" || p.ViewerOpen); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	if !required(p.ID, p.PreviousLabel, p.NextLabel, p.CloseLabel, p.ReturnHref) {
		return fmt.Errorf("PhotoGallery: identity and viewer labels required")
	}
	return validatePhotos(p.Items)
}
func PhotoGallery(p PhotoGalleryProps) g.Node { return PhotoGalleryWithSlots(p, StateSlots{}) }
func PhotoGalleryWithSlots(p PhotoGalleryProps, slots StateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "photo-gallery", p.Label, stateBody(p.State, slots))
	}
	selected := -1
	var thumbs, templates []g.Node
	for i, item := range p.Items {
		ready := item.Full.Status == "" || item.Full.Status == MediaReady
		if item.ID == p.SelectedID && ready {
			selected = i
		}
		link := []g.Node{g.Attr("data-photo-index", itoa(i)), g.Attr("aria-label", item.PositionText), photoMedia(item.Media)}
		if ready && !p.Disabled {
			link = append(link, g.Attr("data-photo-open", p.ID))
		}
		thumbs = append(thumbs, photoThumb(p.Disabled, item.Href, link...))
		if ready && !p.Disabled {
			templates = append(templates, h.Template(g.Attr("data-photo-template", itoa(i)), g.Attr("data-photo-position", item.PositionText), photoMedia(item.Full)))
		}
	}
	var viewer g.Node
	if len(p.Items) > 0 && !p.Disabled {
		index := max(selected, 0)
		var image g.Node
		if selected >= 0 && p.ViewerOpen {
			image = photoMedia(p.Items[selected].Full)
		}
		control := func(label, direction string, disabled bool) g.Node {
			return recoveryAction(ButtonProps{ComponentProps: ComponentProps{Disabled: disabled, Attrs: map[string]string{"data-photo-direction": direction}}, Label: label, Variant: "outline"}, false)
		}
		position := ""
		if selected >= 0 {
			position = p.Items[selected].PositionText
		}
		viewer = ModalWithSlots(ModalProps{ComponentProps: ComponentProps{ID: p.ID + "-viewer", Attrs: map[string]string{"data-photo-viewer": p.ID, "data-photo-selected": itoa(index)}}, AriaLabel: p.Label, CloseLabel: p.CloseLabel, Open: p.ViewerOpen && selected >= 0, Centered: new(true), Size: "large", ShowClose: new(true)},
			ModalSlots{Body: []g.Node{h.Div(g.Attr("data-photo-content", ""), image), h.P(h.Role("status"), g.Attr("data-photo-position", ""), g.Text(position))}, Footer: []g.Node{control(p.PreviousLabel, "-1", index == 0), control(p.NextLabel, "1", index == len(p.Items)-1), recoveryAction(ButtonProps{Label: p.CloseLabel, Href: p.ReturnHref, ComponentProps: ComponentProps{Attrs: map[string]string{"data-photo-close": ""}}}, false)}})
	}
	return sharedSection(p.ComponentProps, "photo-gallery", p.Label, stateBody(p.State, slots), Grid(GridProps{Columns: "1", SM: "2", LG: "3", Gap: "4"}, thumbs...), g.Group(templates), viewer)
}

func (p MasonryProps) Validate() error {
	if err := aggregateState(p.Label, p.State, len(p.Items) > 0 || p.More != nil); err != nil {
		return err
	}
	if !p.State.ready() {
		return nil
	}
	for _, n := range []int{p.Columns, p.SMColumns, p.LGColumns} {
		if n < 0 || n > 4 {
			return fmt.Errorf("Masonry: columns must be 1 through 4")
		}
	}
	if p.Gap != "" {
		if _, ok := clGapScale[p.Gap]; !ok {
			return fmt.Errorf("Masonry: gap must be a shared spacing token")
		}
	}
	if p.More != nil {
		if err := validateChoices([]ChoiceLink{*p.More}); err != nil {
			return err
		}
	}
	return validatePhotos(p.Items)
}
func Masonry(p MasonryProps) g.Node { return MasonryWithSlots(p, MasonrySlots{}) }
func MasonryWithSlots(p MasonryProps, slots MasonrySlots) g.Node {
	if err := p.Validate(); err != nil {
		return invalidComponent(err)
	}
	if !p.State.ready() {
		return sharedSection(p.ComponentProps, "masonry", p.Label, stateBody(p.State, slots.StateSlots))
	}
	var items []g.Node
	for _, item := range p.Items {
		var action g.Node
		if slots.ItemActions != nil {
			action = slots.ItemActions(item)
		}
		items = append(items, h.Div(g.Attr("data-masonry-item", ""), h.Class(style.New().MarginBottom(gapOr(p.Gap, style.S4)).Compile()),
			photoThumb(p.Disabled, item.Href, g.Attr("aria-label", item.PositionText), photoMedia(item.Media)), action))
	}
	base, sm, lg := p.Columns, p.SMColumns, p.LGColumns
	if base == 0 {
		base = 2
	}
	if sm == 0 {
		sm = 3
	}
	if lg == 0 {
		lg = 4
	}
	layout := h.Div(g.Attr("data-masonry-columns", strconv.Itoa(base)), g.Attr("data-masonry-sm", strconv.Itoa(sm)), g.Attr("data-masonry-lg", strconv.Itoa(lg)), h.Class(style.New().Gap(gapOr(p.Gap, style.S4)).Compile()), g.Group(items))
	var more g.Node
	if p.More != nil {
		more = choiceLinks([]ChoiceLink{*p.More}, p.Disabled)
	}
	return sharedSection(p.ComponentProps, "masonry", p.Label, stateBody(p.State, slots.StateSlots), layout, more)
}
