package resource

import (
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/forms"
)

func (c Command) viewRef() screen.ViewRef {
	return genericRef(c.ScreenOwner, c.Entity, "command", c.Verb)
}

func (c Command) viewAttrs() map[string]string {
	if ref := c.viewRef(); ref != "" {
		return map[string]string{"data-screen-view": string(ref)}
	}
	return nil
}

func (c Command) fieldAnchor(path ...string) screen.AnchorRef {
	ref := c.viewRef()
	if ref == "" {
		return ""
	}
	for _, control := range present.CommandView(c.input(c.Present)).Controls {
		if control.Name == "field" && slices.Equal(control.Path, path) {
			anchor, err := ref.Anchor("field", path...)
			if err != nil {
				panic(err)
			}
			return anchor
		}
	}
	return ""
}

func (c Command) formField(words Words, f entity.Field, o Options) forms.Field {
	field := formField(words, f, o)
	field.Anchor = c.fieldAnchor(f.Name)
	return field
}

func (c Command) submitAttrs() map[string]string {
	if c.viewRef() == "" || present.CommandView(c.input(c.Present)).Cardinality == screen.Many {
		return nil
	}
	ref, err := c.viewRef().Anchor("command", c.Verb)
	if err != nil {
		panic(err)
	}
	return map[string]string{"data-screen-anchor": string(ref)}
}
