package contracts

// The media library's own decisions, as functions rather than as SQL: what a
// file's family is, what a caption may claim, how the grid may be ordered, and
// what the tenant's disk says. The SQL service and filetest.Fake both call these
// instead of each keeping a copy — a filter whose meaning lives in a query string
// is a filter a second implementation is free to disagree with, and the module's
// conformance suite would pass while production answered something else.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// The three families the library filters by. They are names of what a person
// would recognise — a picture, a document, a video — and not of media types:
// the filter's meaning has to survive a file whose type somebody mistyped,
// which is what MediaFamily is for.
const (
	FamilyImage    = "image"
	FamilyDocument = "document"
	FamilyVideo    = "video"
)

// Families is every value the type filter accepts, `all` included, in the order
// a control would list them. It is exported so a page draws the same four the
// service refuses anything outside of.
var Families = []string{"all", FamilyImage, FamilyDocument, FamilyVideo}

// ImageTypes and VideoTypes are the two families' closed sets of media types,
// read off the renderable list rather than written beside it: what a browser may
// be shown inline and what the library may call a picture or a video are the same
// two questions, and a second list would be a second answer to drift from the
// first. The essence — the type without its parameters — is what both compare.
//
// image/svg+xml is in neither, and that is the point rather than an oversight: it
// is not renderable here, nothing decodes it and nothing measures it, so it is a
// document and the image filter cannot be used to smuggle a script onto the grid.
var (
	ImageTypes = renderableOf("image/")
	VideoTypes = renderableOf("video/")
)

func renderableOf(prefix string) []string {
	var out []string
	for _, t := range renderable {
		if strings.HasPrefix(t, prefix) {
			out = append(out, t)
		}
	}
	return out
}

// Essence is a stored media type without its parameters, lower-cased and
// trimmed — "image/jpeg" of "Image/JPEG; charset=binary". It is the string every
// decision in this file reads, and the SQL side of the same decision writes the
// identical expression over the column, so a type somebody padded cannot land
// in one family at the door and another in the grid.
func Essence(contentType string) string {
	essence := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(essence, ';'); i >= 0 {
		essence = strings.TrimSpace(essence[:i])
	}
	return essence
}

// MediaFamily answers which of the three families a stored row belongs to.
//
// A picture is a row whose media type is one this deployment can render inline
// and whose frame the image pass actually measured: an SVG is not a picture to
// this module — nothing decodes it, nothing gives it a width, and a tile for it
// would be a broken image with a filter's blessing — and a file that declared
// image/jpeg and arrived as something else has no width either, so both fall to
// document. The width is therefore not decoration in the signature: it is what
// separates "this is a photograph" from "somebody typed photograph".
func MediaFamily(contentType string, width int) string {
	essence := Essence(contentType)
	switch {
	case slices.Contains(ImageTypes, essence) && width > 0:
		return FamilyImage
	case slices.Contains(VideoTypes, essence):
		return FamilyVideo
	default:
		return FamilyDocument
	}
}

// SortNewest and its four siblings are the orders the library offers. A sort a
// caller invents is refused rather than passed to a query builder, because the
// list is the one read a stranger types into.
const (
	SortNewest     = "-createdAt"
	SortOldest     = "createdAt"
	SortName       = "name"
	SortNameDesc   = "-name"
	SortSize       = "size"
	SortSizeDesc   = "-size"
	SortDefaultKey = "-createdAt"
)

// Sorts is the allow-list, in the order a control lists it.
var Sorts = []string{SortNewest, SortOldest, SortName, SortNameDesc, SortSize, SortSizeDesc}

// LibrarySort reads a sort token into the entity field to order by and whether
// the order is descending. The empty token is the default, newest first.
// Anything outside the six is ErrInvalid naming all six, because a grid that
// quietly ignored a bad sort would show a person an order they did not ask for.
func LibrarySort(spec string) (field string, desc bool, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "CreatedAt", true, nil
	}
	if !slices.Contains(Sorts, spec) {
		return "", false, fmt.Errorf("%w: sort %q is not one of %s", crud.ErrInvalid, spec, strings.Join(Sorts, ", "))
	}
	field, desc = spec, false
	if strings.HasPrefix(spec, "-") {
		field, desc = spec[1:], true
	}
	switch field {
	case "createdAt":
		field = "CreatedAt"
	case "name":
		field = "Name"
	case "size":
		field = "Size"
	}
	return field, desc, nil
}

// MaxQuery is how much of a search box is a search: a name is a short label,
// and a longer pattern is somebody pasting a query into a box that does not
// speak it.
const MaxQuery = 64

// LibraryQuery is the grid's read: which rows, in which order, how many.
//
// The zero value is the first page of everything, newest first. The service
// applies each filter that is set; the filters compose, so a person searching
// "invoice" inside documents with the unused filter on gets the intersection
// and not three screens each ignoring one of the claims.
type LibraryQuery struct {
	// Name is a substring of a file's name, matched case- and
	// accent-insensitively. Empty means no name filter.
	Name string
	// Family is one of Families, or empty (or "all") for everything.
	Family string
	// Unused keeps only the rows the use ledger stamped — which is the honest
	// reading of UnusedSince: the ledger spoke and said nothing shows it. A row
	// the ledger never mentioned is not "unused" here, because nothing measured
	// when it stopped being shown.
	Unused bool
	// Sort is a token LibrarySort reads.
	Sort string
	// Limit and Offset page the result; Limit 0 is DefaultLibraryPageSize.
	Limit, Offset int
}

// Normalize is the read's own check: it trims what is trimmable, applies the
// defaults and refuses what the caller can fix. Every refusal is ErrInvalid —
// correctable, by typing something else — and names the accepted values.
func (q LibraryQuery) Normalize() (LibraryQuery, error) {
	q.Name = strings.TrimSpace(q.Name)
	q.Family = strings.ToLower(strings.TrimSpace(q.Family))
	if q.Family == "" || q.Family == "all" {
		q.Family = ""
	} else if !slices.Contains(Families, q.Family) {
		return q, fmt.Errorf("%w: type %q is not one of %s", crud.ErrInvalid, q.Family, strings.Join(Families[1:], ", "))
	}
	if utf8.RuneCountInString(q.Name) > MaxQuery {
		return q, fmt.Errorf("%w: a search is at most %d characters, and that one is %d",
			crud.ErrInvalid, MaxQuery, utf8.RuneCountInString(q.Name))
	}
	if _, _, err := LibrarySort(q.Sort); err != nil {
		return q, err
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLibraryPageSize
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q, nil
}

// DefaultLibraryPageSize is the screenful the library draws, the same twenty the
// generated lists draw: a person moving between them should not learn a second
// pagination.
const DefaultLibraryPageSize = 20

// Quota is one tenant's disk: what it holds, what it was given, and the two
// answers a meter draws from that. The arithmetic lives here so the door, the
// page and the fake cannot hold three divisions.
//
// The number is the sum as of the reader's own snapshot. The refusal that
// protects the ceiling is Service.charge's, taken under the tenant's lock inside
// the uploading transaction, and it recomputes the same sum: a meter and a
// refusal can disagree by one commit, and only one of them decides anything.
type Quota struct {
	// Used counts every live row's bytes and every live variant's bytes: one
	// picture at four widths is one tenant's disk.
	Used int64 `json:"used" doc:"Bytes this tenant is holding" readOnly:"true"`
	// Limit is what the deployment gave this tenant; Unlimited says it gave no
	// number at all.
	Limit     int64 `json:"quota" doc:"Bytes allowed, when the deployment sets one" readOnly:"true"`
	Unlimited bool  `json:"unlimited" doc:"True when this installation sets no ceiling" readOnly:"true"`
	// Variants counts the extra widths stored beside the frames, so a panel can
	// say what part of the used number is copies.
	Variants int64 `json:"variants" doc:"Bytes of those bytes that are other widths of the same pictures" readOnly:"true"`
}

// Left is what still fits. An installation with no ceiling has no remainder to
// report, and -1 says so without pretending there is an infinite number.
func (q Quota) Left() int64 {
	if q.Unlimited {
		return -1
	}
	return max(q.Limit-q.Used, 0)
}

// Fraction is the meter's fill, 0 through 1. An unlimited installation and a
// tenant with no rows both draw nothing, which is why an empty ceiling answers
// zero rather than a division by zero.
func (q Quota) Fraction() float64 {
	if q.Unlimited || q.Limit <= 0 || q.Used <= 0 {
		return 0
	}
	if q.Used >= q.Limit {
		return 1
	}
	return float64(q.Used) / float64(q.Limit)
}

// Full is the error state: nothing more fits.
func (q Quota) Full() bool { return !q.Unlimited && q.Limit > 0 && q.Used >= q.Limit }

// AltClaim is the whole of what a caption may assert, in one place, because two
// commands (the entity's own check and the write that captions a picture) and
// the picker's gate all ask the same question and must not hold three answers.
//
// A picture either says what is in it or says it is decorative. Both at once is
// a contradiction a reader would have to arbitrate, and neither is an image a
// body cannot describe, which is why the empty caption that is not declared
// decorative is refused at the command and not at the upload: an arrival has no
// words yet, and inventing them is not this module's to do.
func AltClaim(alt string, decorative bool) error {
	alt = strings.TrimSpace(alt)
	switch {
	case decorative && alt != "":
		return fmt.Errorf("%w: a decorative image says nothing about itself, and that one carries %d characters", crud.ErrInvalid, utf8.RuneCountInString(alt))
	case decorative:
		return nil
	case alt == "":
		return fmt.Errorf("%w: an image either says what is in it or says it is decorative", crud.ErrInvalid)
	case utf8.RuneCountInString(alt) > MaxAlt:
		return fmt.Errorf("%w: an alt text is at most %d characters, and that one is %d", crud.ErrInvalid, MaxAlt, utf8.RuneCountInString(alt))
	}
	return nil
}

// PickerField is the caller's field name, checked rather than sanitised: the name
// comes back as a query parameter's key, so anything outside a plain identifier is a
// request that cannot mean what it says. It is refused as ErrInvalid — typing another
// name is the cure — and the empty name is refused too, because a picker that guessed
// which field it answered would put a picture somewhere its owner never asked for.
func PickerField(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !pickerFieldShape.MatchString(name) {
		return "", fmt.Errorf("%w: a field name is a lower-case identifier — letters, digits, dot and underscore, starting with a letter or digit, and %q is not one", crud.ErrInvalid, name)
	}
	return name, nil
}

// pickerFieldShape is the grammar above: 1 to 64 characters, the first of them not a
// separator. It is a regexp rather than a loop because the whole of it is one line and
// the loop would be six.
var pickerFieldShape = regexp.MustCompile(`^[a-z0-9][a-z0-9_.]{0,63}$`)

// MaxPickerReturn is how long a return address is: a workspace path, and a workspace
// path longer than this is a caller pasting a query into the wrong box.
const MaxPickerReturn = 200

// PickerReturns is the one check the picker applies to the address it may send a
// person to, and it is applied at render, before any "Use image" control is drawn.
//
// A bare same-site workspace path is the whole of what is admissible: no scheme, no
// authority, no fragment, no query (the id is the only value this page puts in a URL,
// and a caller's query would be a payload this one cannot see), no backslash, no control
// character. `/app/` is the prefix because a picker's caller is a workspace form, and
// anything outside the workspace is not a place this module will navigate a person to.
// The content-security-policy's `form-action 'self'` is the second wall, not the first:
// a page that drew a button whose action nobody vouched for would still be a page that
// lied about a door to a caller who reads the markup rather than the headers.
func PickerReturns(path string) (string, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "" || utf8.RuneCountInString(path) > MaxPickerReturn:
		return "", fmt.Errorf("%w: the address to return to is a workspace path of at most %d characters", crud.ErrInvalid, MaxPickerReturn)
	case strings.ContainsAny(path, "?#\\"):
		return "", fmt.Errorf("%w: %s is not a bare path; a query, a fragment and an escape have no part in an address this picker will navigate to", crud.ErrInvalid, path)
	case strings.IndexFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return "", fmt.Errorf("%w: the address to return to carries a control character", crud.ErrInvalid)
	case !strings.HasPrefix(path, "/app/"):
		return "", fmt.Errorf("%w: %s is not a workspace path; the picker returns to an address under /app/", crud.ErrInvalid, path)
	}
	return path, nil
}

// PickerParameter is the query key the answer arrives under: the caller's own field
// name, prefixed so the parameter cannot be mistaken for one the called page declared.
// It is one function because the picker's form and every sentence about it must spell
// the key the same way.
func PickerParameter(field string) string { return "pick." + field }

// Pickable is whether "Use image" may be allowed to answer this row, and it is the whole
// of the gate a picture has to pass to reach a body: it is an image this deployment can
// draw, its frame was actually measured, and it either says what is in it or says that it
// is decorative. The third clause is the reason the library refuses an uncaptioned picture
// to a picker while the grid shows it: the grid is where its caption gets written.
//
// There is no fourth condition: no retention class, no visibility, no client.
func Pickable(f *File) bool {
	return f != nil && MediaFamily(f.ContentType, f.Width) == FamilyImage &&
		f.Width > 0 && (f.Decorative || strings.TrimSpace(f.Alt) != "")
}

// Library is what the media library asks of this module's service: the grid's
// read, the meter, and the caption. It is a type beside Service rather than a
// third, fourth and fifth method on it for the reason contracts.WidthOpener
// exists: an exported interface that gains methods is a break for anybody who
// implemented the old one, and a caller that draws a grid has no business being
// handed the erasure commands.
//
// Every read is scoped by the caller's transaction — the same row-level policy
// that answers everything else here — so a file of another tenant is not found,
// not empty, and never listed.
type Library interface {
	// Search is the grid: the tenant's rows, filtered by name, family and the
	// unused stamp, ordered by one of the six LibrarySort offers, one page of
	// them, with the total behind the page.
	//
	// A variant is never an entry: it has no files row, so there is nothing to
	// list, nothing to select and no second charge. The panel reports the widths
	// of the frame it belongs to.
	Search(ctx context.Context, tx db.Tx[db.Tenant], q LibraryQuery) ([]*File, int64, error)

	// Quota is the meter: what this tenant holds and what it was given, taken
	// without the upload lock on purpose, because a page load that queued behind
	// somebody's uploads would be an outage with a friendly face. It answers the
	// same sum Service.charge refuses against, so the two can differ by one
	// commit and never by a different rule. An installation with no ceiling
	// answers Unlimited and the page draws no bar.
	Quota(ctx context.Context, tx db.Tx[db.Tenant]) (*Quota, error)

	// SetAlt writes what a picture says about itself, or its absence, inside the
	// caller's transaction, and publishes file.altered.
	//
	// expected is the UpdatedAt the caller was shown. A row that has moved past
	// it answers ErrConflict and changes nothing: a caption typed over three
	// minutes of somebody else's edit is worth a re-read, and a command that
	// silently overwrote it would make the panel's own number meaningless. The
	// refusal writes no row and publishes nothing.
	SetAlt(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, alt string, decorative bool, expected time.Time) (*File, error)
}
