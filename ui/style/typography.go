package style

// FontSize is a typed type-scale level matching tokens.TypographyScale.
type FontSize string

const (
	TextXS   FontSize = "xs"
	TextSM   FontSize = "sm"
	TextBase FontSize = "base"
	TextLG   FontSize = "lg"
	TextXL   FontSize = "xl"
	Text2XL  FontSize = "2xl"
	Text3XL  FontSize = "3xl"
	Text4XL  FontSize = "4xl"
	Text5XL  FontSize = "5xl"
	Text6XL  FontSize = "6xl"
	Text7XL  FontSize = "7xl"
	Text8XL  FontSize = "8xl"
	Text9XL  FontSize = "9xl"
)

// FontWeight is a typed font-weight step.
type FontWeight string

const (
	FontThin       FontWeight = "thin"       // 100
	FontExtralight FontWeight = "extralight" // 200
	FontLight      FontWeight = "light"      // 300
	FontNormal     FontWeight = "normal"     // 400
	FontMedium     FontWeight = "medium"     // 500
	FontSemibold   FontWeight = "semibold"   // 600
	FontBold       FontWeight = "bold"       // 700
	FontExtrabold  FontWeight = "extrabold"  // 800
	FontBlack      FontWeight = "black"      // 900
)

// Tracking is a typed letter-spacing step.
type Tracking string

const (
	TrackingTighter Tracking = "tighter"
	TrackingTight   Tracking = "tight"
	TrackingNormal  Tracking = "normal"
	TrackingWide    Tracking = "wide"
	TrackingWider   Tracking = "wider"
	TrackingWidest  Tracking = "widest"
)

// Leading is a typed line-height step.
type Leading string

const (
	LeadingNone    Leading = "none"
	LeadingTight   Leading = "tight"
	LeadingSnug    Leading = "snug"
	LeadingNormal  Leading = "normal"
	LeadingRelaxed Leading = "relaxed"
	LeadingLoose   Leading = "loose"
)

// FontFamily is a typed font-family utility (Tailwind's font-sans /
// font-serif / font-mono).
type FontFamily string

const (
	FontSans  FontFamily = "sans"
	FontSerif FontFamily = "serif"
	FontMono  FontFamily = "mono"
)

// AllFontFamilies returns every FontFamily const in stable order.
func AllFontFamilies() []FontFamily {
	return []FontFamily{FontSans, FontSerif, FontMono}
}

// TextAlign is a typed text alignment.
type TextAlign string

const (
	TextLeft    TextAlign = "left"
	TextCenter  TextAlign = "center"
	TextRight   TextAlign = "right"
	TextJustify TextAlign = "justify"
)

// AllFontSizes returns every FontSize const in stable order.
func AllFontSizes() []FontSize {
	return []FontSize{
		TextXS, TextSM, TextBase, TextLG, TextXL,
		Text2XL, Text3XL, Text4XL, Text5XL,
		Text6XL, Text7XL, Text8XL, Text9XL,
	}
}

// AllFontWeights returns every FontWeight const in stable order.
func AllFontWeights() []FontWeight {
	return []FontWeight{
		FontThin, FontExtralight, FontLight, FontNormal,
		FontMedium, FontSemibold, FontBold, FontExtrabold, FontBlack,
	}
}

// AllTrackings returns every Tracking const in stable order.
func AllTrackings() []Tracking {
	return []Tracking{
		TrackingTighter, TrackingTight, TrackingNormal,
		TrackingWide, TrackingWider, TrackingWidest,
	}
}

// AllLeadings returns every Leading const in stable order.
func AllLeadings() []Leading {
	return []Leading{
		LeadingNone, LeadingTight, LeadingSnug,
		LeadingNormal, LeadingRelaxed, LeadingLoose,
	}
}

// AllTextAligns returns every TextAlign const in stable order.
func AllTextAligns() []TextAlign {
	return []TextAlign{TextLeft, TextCenter, TextRight, TextJustify}
}
