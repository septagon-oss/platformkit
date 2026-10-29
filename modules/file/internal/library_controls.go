package internal

import (
	"context"
	"time"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// captionForm preserves the stored caption and expected revision in one accessible form.
func captionForm(ctx context.Context, r page.Request, where string, f *contracts.File) g.Node {
	return h.Form(h.Method("post"), h.Action(where+"/"+f.ID.String()+"/alt"),
		g.Attr("aria-label", said(r, "file.library.caption.label", "What this image says about itself")),
		g.Attr("data-file-caption", ""),
		components.Textarea(components.TextareaProps{
			ComponentProps: components.ComponentProps{Attrs: libraryAnchor("field.alt")},
			Name:           "alt", Value: f.Alt, Rows: 3, MaxLength: contracts.MaxAlt,
			Label: said(r, "file.library.caption.label", "What this image says about itself"),
			HelperText: said(r, "file.library.altRequired",
				"An image either says what is in it or says that it is decorative."),
			Required: !f.Decorative,
		}),
		components.Checkbox(components.CheckboxProps{
			ComponentProps: components.ComponentProps{Attrs: libraryAnchor("field.decorative")},
			Name:           "decorative", Checked: f.Decorative,
			Label: said(r, "file.library.decorative", "Decorative image"),
			HelpText: said(r, "file.library.decorativeHelp",
				"Checked, the image says nothing about itself and no text is asked for."),
		}),
		h.Input(h.Type("hidden"), h.Name("updated"), h.Value(f.UpdatedAt.UTC().Format(time.RFC3339Nano))),
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: libraryAnchor("action.save-alt")},
			Type:           "submit", Variant: "primary",
			Label: said(r, "file.library.caption.save", "Save caption"),
		}),
		page.InlineScript(ctx, captionToggle),
	)
}

// captionToggle keeps the required state in sync with the decorative checkbox.
const captionToggle = `(()=>{const f=document.currentScript.closest("form");if(!f)return;const d=f.querySelector("[name=decorative]"),a=f.querySelector("[name=alt]");if(!d||!a)return;const sync=()=>{a.required=!d.checked;a.setAttribute("aria-required",String(!d.checked));if(d.checked)a.value=""};d.addEventListener("change",sync);sync()})()`

// removalForm explains why a referenced file cannot be deleted.
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
			ComponentProps: components.ComponentProps{Disabled: shown, Attrs: libraryAnchor("action.delete")},
			Type:           "submit", Label: said(r, "file.library.delete", "Remove this file"),
			Tone: "danger", Variant: "outline",
		}),
	)
}
