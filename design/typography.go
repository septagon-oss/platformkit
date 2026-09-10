package design

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// FontFamily names either a literal family or a generic fallback. A quoted
// CSS family such as "serif" is a literal name, not the generic serif family.
// Neither form identifies available font bytes or an individual face.
type FontFamily struct {
	Name    string `json:"name"`
	Generic bool   `json:"generic,omitzero"`
}

// Validate checks typed family meaning without parsing Name as CSS. Literal
// punctuation and whitespace remain part of the name; generic keywords use
// canonical ASCII lowercase. This checks no installed faces or font assets.
func (f FontFamily) Validate() error {
	if f.Name == "" || !utf8.ValidString(f.Name) || strings.ContainsAny(f.Name, "\x00\n\r\f") ||
		(f.Generic && (!fontGeneric(f.Name) || f.Name != fontKeyword(f.Name))) {
		return fmt.Errorf("invalid typed font family %q", f.Name)
	}
	return nil
}

// FontFamilyToken preserves an existing theme token's ordered fallback list.
type FontFamilyToken struct {
	Name     string       `json:"name"`
	Families []FontFamily `json:"families"`
}

// Validate requires a CSS custom-property identity and ordered family values.
func (t FontFamilyToken) Validate() error {
	if _, err := (ColorValue{Reference: t.Name}).CSS(); err != nil || len(t.Families) == 0 {
		return fmt.Errorf("invalid font family token %q", t.Name)
	}
	for _, family := range t.Families {
		if err := family.Validate(); err != nil {
			return fmt.Errorf("font family token %s: %w", t.Name, err)
		}
	}
	return nil
}

// FontFamilies projects the same resolved typography that Tokens and CSS use.
// It accepts unescaped quoted names, unquoted CSS identifiers, and the common
// generic keywords in CSS Fonts 4 (2026-09-07). Escapes, comments, functions,
// system/context keywords and legacy emoji/fangsong keywords are unsupported;
// refusal returns no partial projection. The trusted CSS source is unchanged.
func (t Theme) FontFamilies() ([]FontFamilyToken, error) {
	var out []FontFamilyToken
	for _, token := range t.Tokens() {
		if token.Type != "fontFamily" {
			continue
		}
		families, err := ParseFontFamilies(token.Value)
		if err != nil {
			return nil, fmt.Errorf("font family token %s: %w", token.Name, err)
		}
		out = append(out, FontFamilyToken{Name: token.Name, Families: families})
	}
	return out, nil
}

// This is the unescaped identifier subset, not a second CSS parser. ASCII
// whitespace separates identifiers; non-ASCII space remains part of a name.
var fontIdentifier = regexp.MustCompile(`^(--|-?[a-zA-Z_\x{80}-\x{10ffff}])[-a-zA-Z_0-9\x{80}-\x{10ffff}]*$`)

const fontWhitespace = " \t\n\r\f"

// ParseFontFamilies decodes the same admitted fallback-stack subset used by
// Theme.FontFamilies. It returns detached names or an error with no partial list;
// callers binding a typed projection to a CSS token need not parse it again.
func ParseFontFamilies(value string) ([]FontFamily, error) {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00") {
		return nil, fmt.Errorf("invalid encoding or unsupported CSS escape")
	}
	var out []FontFamily
	for {
		value = strings.Trim(value, fontWhitespace)
		if value == "" {
			return nil, fmt.Errorf("empty family")
		}
		var family FontFamily
		if quote := value[0]; quote == '\'' || quote == '"' {
			name, rest, closed := strings.Cut(value[1:], string(quote))
			if !closed || name == "" || strings.ContainsAny(name, "\n\r\f") {
				return nil, fmt.Errorf("invalid quoted family")
			}
			family.Name, value = name, strings.Trim(rest, fontWhitespace)
		} else {
			name, rest, comma := strings.Cut(value, ",")
			words := strings.FieldsFunc(name, func(r rune) bool { return strings.ContainsRune(fontWhitespace, r) })
			if len(words) == 0 {
				return nil, fmt.Errorf("empty family")
			}
			for _, word := range words {
				if !fontIdentifier.MatchString(word) {
					return nil, fmt.Errorf("unsupported family identifier %q", word)
				}
				if fontGeneric(word) && len(words) != 1 || fontContextKeyword(word) {
					return nil, fmt.Errorf("family keyword %q must be quoted", word)
				}
			}
			family.Name = strings.Join(words, " ")
			family.Generic = fontGeneric(family.Name)
			if family.Generic {
				family.Name = strings.ToLower(family.Name)
			}
			value = ""
			if comma {
				value = "," + rest
			}
		}
		out = append(out, family)
		if value == "" {
			return out, nil
		}
		rest, comma := strings.CutPrefix(value, ",")
		if !comma {
			return nil, fmt.Errorf("expected comma after quoted family")
		}
		value = rest
	}
}

func fontGeneric(name string) bool {
	switch fontKeyword(name) {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui", "ui-serif", "ui-sans-serif", "ui-monospace", "ui-rounded", "math":
		return true
	}
	return false
}

func fontContextKeyword(name string) bool {
	switch fontKeyword(name) {
	case "inherit", "initial", "unset", "revert", "revert-layer", "default", "caption", "icon", "menu", "message-box", "small-caption", "status-bar", "emoji", "fangsong":
		return true
	}
	return false
}

// CSS keywords are ASCII-insensitive, not Unicode case-folded family names.
func fontKeyword(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, name)
}
