package contracts

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrTooLarge is an upload past the limit the deployment set. It is a failure
// of its own rather than one of kit/crud's three, because the answer is 413 and
// nothing else in the application has one.
var ErrTooLarge = errors.New("file: larger than this deployment accepts")

// ErrQuota is an upload that would put this tenant past the disk it is allowed.
// It is 413 as well, because the caller's remedy is the same one — send
// something smaller, or delete something first — and a status of its own would
// tell an anonymous caller how much of somebody else's quota is left.
var ErrQuota = errors.New("file: this tenant has no room left")

// ErrTooManyPixels is a frame over the ceiling this deployment will decode. It
// is its own refusal rather than ErrTooLarge because the two come apart: a
// compressed file can be small and the frame it expands to enormous, which is
// the decompression bomb, and the caller's remedy is a smaller frame rather than
// a shorter upload.
var ErrTooManyPixels = errors.New("file: too many pixels to decode")

// ErrNotImage is a file offered as an image that no decoder this module runs
// reads. It is what refuses a vector drawing with a script in it before a byte
// of it is stored, and it names the reason rather than the media type somebody
// typed into a form.
var ErrNotImage = errors.New("file: not an image this deployment decodes")

// DefaultMaxImagePixels is the frame a deployment decodes unless it says
// otherwise: forty megapixels, which is above any camera anybody uploads and
// below the point where one decode is a heap the request cannot be trusted with.
// It is a count of pixels and not of bytes for the reason above.
const DefaultMaxImagePixels = 40_000_000

// ErrNoBlob is a key Storage has nothing at. It is what a Get answers with when
// the row says there are bytes and there are not, which is the one inconsistency
// the split between a row and a blob can produce.
var ErrNoBlob = errors.New("file: no bytes at this key")

// ErrPublicFile is a grant asked for on a file anybody can already read.
var ErrPublicFile = errors.New("file: this file is public and needs no grant")

// ErrNotShown is a publish that named a file no use row of that record shows.
// It is a conflict and not a validation failure: the remedy is to save the body
// that names the image, which is the door that files uses, and then publish.
var ErrNotShown = errors.New("file: this record shows no use of that file")

// ErrNotSignable is a grant asked for of a store that cannot hand one out. It is
// a wiring fact, not something the caller can change.
var ErrNotSignable = errors.New("file: this deployment's store cannot hand out a time-limited URL")

// ErrInvalidExpiry is a grant asked for outside the bounds this module mints.
var ErrInvalidExpiry = errors.New("file: that expiry is not one this module signs")

// ErrHeld is a removal refused because a hold says the bytes must stay.
var ErrHeld = errors.New("file: this file is under a retention hold")

// ErrInUse is a removal refused because some record still shows the file. It is
// a conflict and not a validation failure, and the remedy is the other door: edit
// or delete the records that name it, and the file goes. A hold outranks a use,
// so a file under a live hold still answers ErrHeld.
var ErrInUse = errors.New("file: this file is shown by records that still exist")

// InUse is that refusal with its size: how many records show the file. Records,
// not rows — one page can show one picture in its body and its summary, in two
// locales, and the person deciding what to do about it counts pages.
type InUse struct {
	File    uuid.UUID
	Records int
}

func (e *InUse) Error() string {
	noun := "records"
	if e.Records == 1 {
		noun = "record"
	}
	return fmt.Sprintf("%s: %s is shown by %d %s", ErrInUse, e.File, e.Records, noun)
}

// ErrBusyDecoding is an image upload the process will not take right now:
// every decode slot is held, and the wait for one ran out. Neither the request
// nor anybody's state is wrong, so it is a 503 with the caller's own retry, not
// a 4xx about a body and not a 500 about a broken installation.
var ErrBusyDecoding = errors.New("file: this instance is decoding every image it has room for")

// ErrUnprovable is an erasure the store cannot certify: it reported a version,
// a delete marker or an in-flight multipart part, so the bytes are not gone in
// the only sense this module can check. The answer is the bucket's posture and
// no caller-side change reaches past it.
var ErrUnprovable = errors.New("file: the store keeps copies this module cannot remove")
