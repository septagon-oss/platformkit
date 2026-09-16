package style

// values.go reads the scales back as CSS text, for the one kind of rule the
// class builder cannot express: a consumer sheet addressed by element rather
// than by class, such as the web module's prose styles for rendered Markdown.
// Each accessor returns exactly the literal the corresponding utility rule
// declares, so an element rule and a utility rule for the same step cannot
// drift apart; the tests below check that agreement against Rules.

// Value is the CSS length or keyword the utilities for this step declare:
// S4 is "1rem", SPX is "1px", SFull is "100%", SAuto is "auto".
func (s Spacing) Value() string {
	v, _ := spacingCSS(string(s))
	return v
}

// Value is the font-size the text utility for this level declares, without
// the line-height that utility sets beside it.
func (f FontSize) Value() string {
	return fontSizes[string(f)][0]
}

// Value is the border-radius the rounded utility for this step declares.
func (r Radius) Value() string {
	key := string(r)
	if r == RadiusBase {
		key = ""
	}
	return radii[key]
}

// Value is the border-width the border utility for this step declares.
func (b BorderWidth) Value() string {
	if b == Border1 {
		return "1px"
	}
	return string(b) + "px"
}
