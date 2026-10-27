package internal

import (
	"github.com/septagon-oss/platformkit/modules/admin/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func screenAnchor(view, name string) map[string]string {
	return map[string]string{"data-screen-anchor": string(contracts.Screen(view)) + "#" + name}
}

func loginWays(text func(string, string) string, forgot, register string) []g.Node {
	ways := []g.Node{components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("login", "action.forgot")}, Href: forgot, Variant: "text", Label: text("forgot", "Forgot your password?")})}
	if register != "" {
		ways = append(ways, components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: screenAnchor("login", "action.register")}, Href: register, Variant: "text", Label: text("register", "Create an account")}))
	}
	return ways
}
