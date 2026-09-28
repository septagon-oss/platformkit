package design

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Client is what one client's design.yaml says about its identity: a slug, a
// seed, and at most a handful of named token overrides. It carries no file
// format beyond struct tags — the loader that decodes it lives with the
// configuration owner, so this package keeps importing nothing but the standard
// library and stays the single place the meaning of a token is decided.
//
// The point of the shape is what it does not allow: no rule, no selector, no
// second stylesheet, and no token outside the twenty-two the theme already
// exports. A client that wants more than this wants a change to the kernel.
type Client struct {
	// Slug names the client, and is the key a set of clients is held under.
	Slug string `yaml:"slug"`
	// Seed is the identity the pair is generated from.
	Seed Seed `yaml:"seed"`
	// Tokens overrides named colour tokens per theme. The key is "light" or
	// "dark"; the inner key is a token name without its --pk-color- prefix.
	Tokens map[string]map[string]string `yaml:"tokens,omitempty"`
	// Typography and Shape are the client's own trusted values, shared by both
	// themes when it names them.
	Typography Typography `yaml:"typography,omitempty"`
	Shape      Shape      `yaml:"shape,omitempty"`
}

// clientThemes are the only two themes a client may write tokens for.
var clientThemes = []string{"light", "dark"}

// typographyFields names the three stacks by the token each one becomes, so a
// refusal says which stack. There is deliberately no second grammar for a font
// stack in this package: ParseFontFamilies is what Theme.FontFamilies and the
// export already read, so a stack a client may write is exactly a stack the
// renderer can project — a looser check here would only move the failure from
// this file to ExportTokens.
var typographyFields = []struct {
	name  string
	value func(Typography) string
}{
	{"display", func(t Typography) string { return t.Display }},
	{"body", func(t Typography) string { return t.Body }},
	{"mono", func(t Typography) string { return t.Mono }},
}

// Validate refuses a client file that could not be applied: no slug, a seed it
// would refuse, a theme or token name outside the vocabulary, a token value that
// is not an opaque colour literal, or a type or shape value that is not a font
// stack or a length. Every override is checked here and again after Resolve,
// against the pair it would have been applied to.
func (c Client) Validate() error {
	if strings.TrimSpace(c.Slug) == "" {
		return fmt.Errorf("design: client design requires a slug")
	}
	if err := c.Seed.Validate(); err != nil {
		return err
	}
	for _, theme := range slices.Sorted(maps.Keys(c.Tokens)) {
		if theme != "light" && theme != "dark" {
			return fmt.Errorf("design: client %s names theme %q, want light or dark", c.Slug, theme)
		}
		for _, token := range slices.Sorted(maps.Keys(c.Tokens[theme])) {
			value := c.Tokens[theme][token]
			color, err := parseColor(value)
			if err != nil {
				return fmt.Errorf("design: client %s theme %s: %w", c.Slug, theme, err)
			}
			// A foreground with alpha has no contrast ratio until it is composited,
			// and measuring it as though it were opaque is how invisible text passes
			// a legibility gate — transparent measures 21:1 on a light canvas. The
			// translucent look a client wanted is a ColorMix of two opaque literals,
			// which is what the kernel models for exactly that.
			if color[3] != 1 {
				return fmt.Errorf("design: client %s theme %s overrides %q with %q, which carries alpha: name an opaque colour, because a colour with alpha has no contrast until it is composited", c.Slug, theme, token, value)
			}
			if _, ok := lookupColor(token); !ok {
				return fmt.Errorf("design: client %s theme %s overrides %q, which no theme exports", c.Slug, theme, token)
			}
		}
	}
	for _, field := range typographyFields {
		if stack := field.value(c.Typography); stack != "" {
			if _, err := ParseFontFamilies(stack); err != nil {
				return fmt.Errorf("design: client %s names a %s font stack that is not one: %w", c.Slug, field.name, err)
			}
		}
	}
	if err := c.Shape.Validate(); err != nil {
		return fmt.Errorf("design: client %s: %w", c.Slug, err)
	}
	return nil
}

// Resolve generates the pair the seed describes, applies this client's
// overrides, and refuses the result unless it still passes Pair.Check. The gate
// runs over the finished pair rather than each override on its own: a token is
// only readable against the surfaces that client actually ships.
//
// The returned Pair is what ui.Compose takes. It is a value, keyed by slug by
// whoever holds a set of clients, so one process can hold many.
func (c Client) Resolve() (Pair, error) {
	if err := c.Validate(); err != nil {
		return Pair{}, err
	}
	pair, err := FromSeed(c.Seed)
	if err != nil {
		return Pair{}, err
	}
	themes := []Theme{pair.Light, pair.Dark}
	for i, theme := range themes {
		theme.Typography = c.Typography
		theme.Shape = c.Shape
		for _, token := range slices.Sorted(maps.Keys(c.Tokens[theme.Name])) {
			theme, err = theme.setColor(token, c.Tokens[theme.Name][token])
			if err != nil {
				return Pair{}, fmt.Errorf("design: client %s theme %s: %w", c.Slug, theme.Name, err)
			}
		}
		if i == 0 {
			pair.Light = theme
		} else {
			pair.Dark = theme
		}
	}
	if err := pair.Check(); err != nil {
		return Pair{}, fmt.Errorf("design: client %s: %w", c.Slug, err)
	}
	return pair, nil
}
