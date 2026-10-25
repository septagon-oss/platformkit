package style

// classFontSize maps FontSize → "text-*".
func classFontSize(f FontSize) string {
	if f == "" {
		return ""
	}
	return "text-" + string(f)
}

// classFontWeight maps FontWeight → "font-*".
func classFontWeight(f FontWeight) string {
	if f == "" {
		return ""
	}
	return "font-" + string(f)
}

// classFontFamily maps FontFamily → "font-*" (sans/serif/mono).
func classFontFamily(f FontFamily) string {
	if f == "" {
		return ""
	}
	return "font-" + string(f)
}

// classTracking maps Tracking → "tracking-*".
func classTracking(t Tracking) string {
	if t == "" {
		return ""
	}
	return "tracking-" + string(t)
}

// classLeading maps Leading → "leading-*".
func classLeading(l Leading) string {
	if l == "" {
		return ""
	}
	return "leading-" + string(l)
}

// classTextAlign maps TextAlign → "text-*".
func classTextAlign(t TextAlign) string {
	if t == "" {
		return ""
	}
	return "text-" + string(t)
}

// classUnderlineOffset emits "underline-offset-<step>".
func classUnderlineOffset(step string) string {
	if step == "" {
		return ""
	}
	return "underline-offset-" + step
}

// classListStyle emits "list-<style>" (decimal/disc/none).
func classListStyle(style string) string {
	if style == "" {
		return ""
	}
	return "list-" + style
}

// classLineClamp emits "line-clamp-N" for a multi-line text clamp.
func classLineClamp(n int) string {
	if n <= 0 {
		return ""
	}
	return "line-clamp-" + itoa(n)
}
