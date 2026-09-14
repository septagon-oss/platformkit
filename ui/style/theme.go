package style

import (
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/css"
)

// ThemeVars is the token layer of the stylesheet: the light theme on :root, the
// dark one under [data-theme="dark"], and the dark one again under a
// prefers-color-scheme query for a browser whose owner has said what they want
// and an application that has not been told otherwise.
//
// The attribute wins over the query, which is the ordering a theme toggle
// needs: setting data-theme="light" on a machine in dark mode has to mean
// light. That is why the media block is qualified by :root:not([data-theme]).
//
// It takes the two themes rather than reading a default pair, so that a client
// with its own colours changes this one argument and nothing else; see
// design.Pair. It lives here rather than beside the themes because design is a
// leaf that describes values and imports nothing of ui, while this package
// already owns the rules written in terms of these properties (RoleVars).
func ThemeVars(light, dark design.Theme) *css.Sheet {
	s := css.NewSheet()
	// Equal resolved stacks inherit once; differing stacks follow the same
	// explicit-mode and system-preference cascade as colours.
	darkFonts := !slices.Equal(fontTokens(light), fontTokens(dark))
	s.Select(":root", append(themeDeclarations(light, true),
		css.Decl("color-scheme", css.Literal("light")),
	)...)
	s.Select(`[data-theme="dark"]`, append(themeDeclarations(dark, darkFonts),
		css.Decl("color-scheme", css.Literal("dark")),
	)...)
	s.Media("(prefers-color-scheme: dark)", func(inner *css.Sheet) {
		inner.Select(`:root:not([data-theme])`, append(themeDeclarations(dark, darkFonts),
			css.Decl("color-scheme", css.Literal("dark")),
		)...)
	})
	return s
}

// themeDeclarations is one theme's tokens as custom property declarations. The
// font stacks are optional because a dark theme that shares the light theme's
// type inherits it from :root instead of declaring it a second and third time.
func themeDeclarations(t design.Theme, includeFonts bool) []css.Declaration {
	var out []css.Declaration
	for _, token := range t.Tokens() {
		if token.Type != "fontFamily" || includeFonts {
			out = append(out, css.Decl(token.Name, css.Literal(token.Value)))
		}
	}
	return out
}

func fontTokens(t design.Theme) []design.Token {
	var out []design.Token
	for _, token := range t.Tokens() {
		if token.Type == "fontFamily" {
			out = append(out, token)
		}
	}
	return out
}
