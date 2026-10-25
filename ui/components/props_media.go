package components

// AvatarProps is a person as a disc: the thing every member row, comment and
// attendee list needs, and therefore the thing every product otherwise hand-rolls
// as a coloured div with a letter in it.
type AvatarProps struct {
	ComponentProps

	// Name is the person as they are called. It is the accessible name of the
	// disc and the source of the initials; nothing here invents a substitute for
	// a caller who left it empty.
	Name string `json:"name,omitempty" maxLength:"120"`
	// Src is their picture, when one exists. It goes through Media, so alt text,
	// lazy loading and the four ways a picture fails are decided once.
	Src string `json:"src,omitempty" maxLength:"2048"`
	// Href makes the disc a link to the person. Link is a text link and cannot
	// hold a picture, so this disc becomes the anchor itself, the way Card does.
	Href string `json:"href,omitempty" maxLength:"2048"`
	Size string `json:"size,omitempty" enum:",sm,md,lg" enumStrict:"true"`
	// Decorative says the name is already on screen beside this disc, so the disc
	// says nothing at all. Left false, the disc carries the name itself.
	Decorative bool `json:"decorative,omitempty"`
	// Label shows the name beside the disc instead of only behind it, which is the
	// form a list cell, a comment row and the generated read axis all need. It is a
	// prop on this component rather than a second component because the alternative
	// is every caller putting a disc and a text node in a box and aligning them
	// differently. With Label set the disc goes quiet and the text carries the name
	// — see Avatar.
	Label string `json:"label,omitempty" maxLength:"120"`
	// AriaLabel names the disc when the caller's word for the person is not the
	// name to show — "Signed out", or a role where a name would be a claim.
	AriaLabel string `json:"ariaLabel,omitempty"`
}

// MediaProps defines one picture and what to say about it while it is not there.
//
// Alt is required unless Decorative is true: a picture that carries information
// with no text alternative is invisible to exactly the people the rest of this
// package is written for. The renderer cannot read the picture to find out, so
// the requirement is stated here, every specimen in the gallery obeys it, and
// e2e/design-audit.spec.ts measures it on the painted page — where it belongs,
// because a rule only in a comment is a preference.
type MediaProps struct {
	ComponentProps

	Status MediaStatus `json:"status,omitempty"`
	Src    string      `json:"src,omitempty"`
	Alt    string      `json:"alt,omitempty"`
	// Decorative marks a picture that says nothing an assistive reader would
	// miss, which is the only honest way to arrive at an empty alt.
	Decorative bool `json:"decorative,omitempty"`
	// Width and Height are the picture's intrinsic size in pixels. They are
	// attributes, not styling: without them the box is 0px tall until the bytes
	// land, and the page underneath jumps when they do.
	// Each on its own line: a struct tag written after a shared field line is
	// shared too, so this as one declaration gave Height the json name "width"
	// and the two collided — which the gallery's own description test caught,
	// and no compiler would have.
	Width   int    `json:"width,omitempty" minimum:"0"`
	Height  int    `json:"height,omitempty" minimum:"0"`
	Caption string `json:"caption,omitempty"`
	// Reason is the capability's own sentence for a failure or a refusal. The
	// renderer holds no words of its own: "the cut-out failed" and "the print
	// shop is closed" are the same state and different news.
	Reason string `json:"reason,omitempty"`
	// Horizontal is a fixed-width thumbnail beside text rather than a full-width
	// image above it. It is one field and not a class string, because the two
	// forms differ in one width and this is the picture Card has always drawn:
	// cardImage delegates here, so there is one place a picture's markup lives.
	Horizontal bool `json:"horizontal,omitempty"`
	// Lazy defers the fetch until the picture is near the viewport, which is what
	// a long list of them wants. Left unset it loads eagerly, because a picture
	// above the fold that waits for a scroll is a picture nobody asked to wait for.
	Lazy bool `json:"lazy,omitempty"`
	// Fit is what the box does with a picture of another shape: "cover" crops to
	// fill it, "contain" keeps the whole subject inside. It is the caller's call
	// because it is a claim about the picture — a face cropped is a different
	// picture — and not about the box. Empty leaves the source's own aspect.
	Fit string `json:"fit,omitempty" enum:",contain,cover" enumStrict:"true"`
}
