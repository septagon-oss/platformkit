package internal

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

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

// uploadQuery is the upload post's whole declared input: the claim the door makes
// about the bytes, spelled as the JSON door spells it. It declares no body because
// nothing decodes one — the file arrives as the multipart part this route reads off
// the request, which is what the route's StreamsBody mark is for.
type uploadQuery struct {
	Image bool `query:"image" default:"false" doc:"Treat these bytes as an image: measure the frame, and refuse what no decoder reads"`
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
			// Secondary, and the reason is the page's own: the design floor allows one
			// filled action above the fold, and the library's is adding a file. Filtering
			// is a read this page is already showing, not the thing to do.
			components.Button(components.ButtonProps{
				Type: "submit", Variant: "secondary", Label: said(r, "file.library.filter.apply", "Apply"),
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

// uploadForm is the library's one write of bytes: a plain multipart form whose action
// is this page's own upload route, which streams the part to storage through the same
// command the JSON upload door performs. The person who submits it is answered by the
// library — the new file's panel, or the grid with the refusal above it — because the
// JSON door answers with the row it wrote, and a raw JSON document is where a person
// who pressed Upload should never end up.
//
// The `image=true` is the door's own claim, not this page's guess about the file: it is
// what turns bytes no decoder reads into a refusal rather than an attachment, and a
// library whose grid is full of attachments nobody asked for is the failure it named.
func uploadForm(r page.Request, where string, may bool) g.Node {
	if !may {
		// A person who may not upload is shown no control, exactly as the caption and
		// removal forms work; the post would refuse them either way.
		return g.Text("")
	}
	return h.Form(h.Method("post"), h.Action(where+"/upload?image=true"), h.EncType("multipart/form-data"),
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
		_, preview := tile(r, bytes, f, nil)
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
