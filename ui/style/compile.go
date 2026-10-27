package style

// Class compilers map typed style values to utility fragments.
// Family-specific compilers live beside these shared prefixes and atoms.

const (
	classPrefixMinH       = "min-h-"
	classPrefixRounded    = "rounded-"
	classPrefixBorder     = "border-"
	classPrefixTranslateY = "translate-y-"
	classPrefixTranslateX = "translate-x-"
)

// prefixOrEmpty returns prefix+v when v is non-empty, or "" when v is empty.
// Shared by multiple class* helper functions to eliminate duplicated empty-check + prefix patterns.
func prefixOrEmpty(v, prefix string) string {
	if v == "" {
		return ""
	}
	return prefix + v
}

// Static utility atoms. Exported names so ClassList methods reference
// these rather than inlining strings.

const (
	classUnderline   = "underline"
	classNoUnderline = "no-underline"
	classSrOnly      = "sr-only"
	classNotSrOnly   = "not-sr-only"
	classMinHScreen  = "min-h-screen"
	classTruncate    = "truncate"
	classRelative    = "relative"
	classAntialiased = "antialiased"
	classFlexShrink0 = "flex-shrink-0"
	classTransform   = "transform"
	classFlex1       = "flex-1"
	classRingInset   = "ring-inset"
	classFlexWrap    = "flex-wrap"
	classFlexNoWrap  = "flex-nowrap"
	classUppercase   = "uppercase"
	classLowercase   = "lowercase"
	classCapitalize  = "capitalize"
	classNormalCase  = "normal-case"
)

// Static additional atoms used 3+ times across components.
const (
	classItalic            = "italic"
	classNotItalic         = "not-italic"
	classAppearanceNone    = "appearance-none"
	classWhitespaceNoWrap  = "whitespace-nowrap"
	classWhitespacePreWrap = "whitespace-pre-wrap"
	classBreakWords        = "break-words"
	classBreakAll          = "break-all"
	classBreakAnywhere     = "break-anywhere"
	classBreakNormal       = "break-normal"
	classTabularNums       = "tabular-nums"
	classObjectCover       = "object-cover"
	classObjectContain     = "object-contain"
	classAspectSquare      = "aspect-square"
	classAspectVideo       = "aspect-video"
	classAnimatePulse      = "animate-pulse"
	classAnimateSpin       = "animate-spin"
	classLineThrough       = "line-through"
	classNoLineThrough     = "no-line-through"
)

// classColSpanFull is Tailwind's "col-span-full" — span every column
// of the current grid regardless of column count.
const classColSpanFull = "col-span-full"

// itoa converts a non-negative int to its decimal string without
// pulling in strconv at the Tailwind-compile layer. The value is
// expected to be a small grid column count.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [10]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

const (
	classFlexGrow          = "flex-grow"
	classFlexGrow0         = "flex-grow-0"
	classGroup             = "group"
	classPeer              = "peer"
	classWhitespacePre     = "whitespace-pre"
	classOverscrollContain = "overscroll-contain"
)

// classFlexNone is Tailwind's "flex-none" atom — prevents a flex
// child from growing or shrinking.
const classFlexNone = "flex-none"
