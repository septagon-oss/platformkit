package forms

import (
	"github.com/septagon-oss/platformkit/kit/screen"
	g "maragu.dev/gomponents"
)

func anchorAttrs(ref screen.AnchorRef) map[string]string {
	if ref == "" {
		return nil
	}
	if _, err := screen.ParseAnchor(string(ref)); err != nil {
		panic(err)
	}
	return map[string]string{"data-screen-anchor": string(ref)}
}

func actionAttrs(view screen.ViewRef, action string) map[string]string {
	if view == "" {
		return nil
	}
	ref, err := view.Anchor("action", action)
	if err != nil {
		panic(err)
	}
	return anchorAttrs(ref)
}

func anchorNodes(ref screen.AnchorRef) []g.Node {
	if ref == "" {
		return nil
	}
	return []g.Node{g.Attr("data-screen-anchor", anchorAttrs(ref)["data-screen-anchor"])}
}
