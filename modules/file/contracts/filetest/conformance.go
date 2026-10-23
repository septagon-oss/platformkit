// Package filetest is the conformance suite for contracts.Service, a Storage
// in memory that the suite runs against, and a fake Service that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification written as executable cases; the real service and the fake both
// run it.
package filetest

import (
	"bytes"
	goimage "image"
	"image/color"
	pngimage "image/png"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"

	"context"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and the storage behind it, so a case can check that the bytes went where the
// row says they did.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Storage is the same store the Service was built on, for the cases about
	// what is left behind: an upload that was refused, and a delete that has
	// not been handled yet.
	Storage contracts.Storage
	// Keys is every key the storage currently holds, which is the one question
	// contracts.Storage does not answer and the only way to see an orphan.
	Keys func() []string
	// Published is the events the implementation has published, in order.
	Published func() []string
	// Remove takes one file row away past every rule Delete keeps. The suite's own
	// doors refuse to do it: Delete refuses a file a record still shows. Two doors
	// in production reach that state without asking — the erasure door and the
	// retention sweep, both named in the module's README — and a case that needs a
	// use whose file is gone can only be written by standing in for them.
	Remove func(id uuid.UUID)
}

// open is the Fixture's transaction as an opener, which is what Upload takes:
// the bytes are stored before anything is opened, so the suite has to be able
// to hand over the opening rather than the opened.
func (f Fixture) open(context.Context) (db.Tx[db.Tenant], error) { return f.Tx, nil }

// scope is the tenant this fixture's context carries. The two harnesses both
// put a tenant on it — the Postgres one because db.Run does, the fake's because
// the fake takes its scope from the context the way the real Upload does — and
// the suite asks for the same scope the command under test would derive.
func (f Fixture) scope(t *testing.T) contracts.Scope {
	t.Helper()
	s, err := contracts.ScopeOf(f.Ctx)
	if err != nil {
		t.Fatalf("the fixture's context names no tenant: %v", err)
	}
	return s
}

func (f Fixture) one(t *testing.T, what, want string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != want {
		t.Errorf("%s published %v, want [%s]", what, got, want)
	}
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// Limit is the largest upload the suite's implementations accept. It is small
// so that a case can go past it without allocating anything worth mentioning.
const Limit = 1 << 10

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// upload is one file arriving, with the body given as a string.
func upload(name, contentType, visibility, body string) contracts.Upload {
	return contracts.Upload{
		Name: name, ContentType: contentType, Visibility: visibility,
		Declared: -1, Body: strings.NewReader(body),
	}
}

// image is an upload the caller called an image: the same envelope as upload,
// with the declaration that decides what a refusal is (contracts.Upload.Image).
// The body is bytes rather than a string because it is a PNG.
func asImage(t *testing.T, name, contentType string, body []byte) contracts.Upload {
	t.Helper()
	return contracts.Upload{
		Name: name, ContentType: contentType, Visibility: contracts.VisibilityPrivate,
		Declared: int64(len(body)), Body: bytes.NewReader(body), Image: true,
	}
}

// pngFrame is a real PNG of w x h with one transparent pixel in it, which is
// what decides its container once the pass re-encodes it.
// pngFrame is an opaque w x h PNG: no alpha, which is what makes the pass
// choose JPEG and the case able to see that it chose.
func pngFrame(t *testing.T, w, h int) []byte {
	t.Helper()
	frame := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame.Set(x, y, color.RGBA{R: uint8(40 + x*30), G: uint8(90 + y*20), B: 200, A: 255})
		}
	}
	out := &bytes.Buffer{}
	if err := pngimage.Encode(out, frame); err != nil {
		t.Fatalf("encode the fixture: %v", err)
	}
	return out.Bytes()
}

// pngOf encodes an RGBA frame with one see-through corner.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	frame := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame.Set(x, y, color.RGBA{R: uint8(40 + x*20), G: uint8(90 + y*20), B: 200, A: 255})
		}
	}
	frame.Set(0, 0, color.RGBA{B: 200, A: 128})
	out := &bytes.Buffer{}
	if err := pngimage.Encode(out, frame); err != nil {
		t.Fatalf("encode the fixture: %v", err)
	}
	return out.Bytes()
}

// stored uploads one private text file and returns its row.
func stored(t *testing.T, f Fixture, body string) *contracts.File {
	t.Helper()
	out, err := f.Service.Upload(f.Ctx, f.open, upload("notes.txt", "text/plain", contracts.VisibilityPrivate, body))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return out
}

// read is everything Open gives back, as a string.
func read(t *testing.T, f Fixture, id uuid.UUID, anonymous bool) (*contracts.File, string) {
	t.Helper()
	row, body, err := f.Service.Open(f.Ctx, f.Tx, id, anonymous)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer body.Close()
	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read the bytes: %v", err)
	}
	return row, string(out)
}

// library is the fixture's implementation seen as the library's reads and
// commands. The suite asks them of the same object it asks Upload of, because
// the grid, the meter and the caption belong to the file service and a second
// implementation of them would be a second set of rules to pass.
func library(t *testing.T, f Fixture) contracts.Library {
	t.Helper()
	lib, ok := f.Service.(contracts.Library)
	if !ok {
		t.Fatalf("this implementation of contracts.Service answers none of contracts.Library")
	}
	return lib
}

// hello is the body most cases upload.
const hello = "hello, files\n"

// png is eight bytes of a real PNG: the signature http.DetectContentType reads.
// A conformance suite that uploaded "not really a png" as image/png used to
// pass, and the upload now refuses it — a file this application would show
// inline has to be what it says it is. See internal.agrees.
const png = "\x89PNG\r\n\x1a\n"

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"an upload is counted and hashed as it goes past": countAndHashUpload,
		"the same bytes twice are two files":              separateRepeatedUploads,
		"an upload past the limit keeps nothing":          refuseOversizeUpload,
		"an upload of exactly the limit is kept":          acceptExactLimitUpload,
		"a private file is not found at the public door":  concealPrivateFile,
		"a public file is served to anybody":              servePublicFile,
		// The bytes have to be what the upload said they were, for the types this
		// application shows in a browser. An HTML document uploaded as an image is the
		// second half of the stored-XSS story the download's Content-Disposition is the
		// first half of: a browser would not run one served inline as image/png, but a
		// proxy that rewrites a type, a caller that saves and opens it, and the next
		// media type somebody adds to the inline set are three ways for it to matter.
		"an upload whose bytes disagree with its type is refused": refuseMIMEMismatch,
		// The image pass runs in both implementations from one function
		// (contracts.ProcessImage): the row carries the frame the server stored and the
		// container follows the pixels rather than the file name, so an opaque frame
		// that arrived as a PNG leaves as a JPEG. That a stored image carries no
		// metadata block is proved over the pass itself, where a fixture with a real
		// EXIF and GPS section can be read back; a shared case that re-encodes a PNG
		// writes the same bytes it was given, which is a deterministic encoder rather
		// than a bug this suite should pretend to see.
		"an image is measured and stored as the pixels it re-encodes to":       reencodeImage,
		"what no decoder reads is refused at the image door and keeps nothing": refuseUndecodableImage,
		// A use is what a body references, rewritten rather than appended to, and the
		// answer to "what reads this file" is exactly the set of rows the last accepted
		// body wrote. Both implementations run the same two decisions
		// (contracts.CollapseRefs and contracts.DiffUses) over their own storage.
		"a record's uses are its body's references, rewritten in place":           replaceRecordUses,
		"the uses of a file that is not there are refused and nothing is written": refuseMissingUse,
		// Ending the use of a file that was removed first is not the refusal above, and a
		// record has to survive the difference: Delete consults the ledger no more than it
		// does today, so the row that names a gone file is ordinary rather than corrupt,
		// and the delete of the record that shows it must go through. Refusing here would
		// strand that record and leave the ledger saying a file nobody has is shown.
		"a use ended of a file that is already gone ends": endUseOfDeletedFile,
		// What a picture says about itself is one command's write, guarded by the row's own
		// clock: a caption written against a row somebody else has already moved is refused
		// and not merged, because the alternative is that the panel's number means nothing
		// and two people's sentences overwrite one another.
		"an alt text is written against the row it was shown": reviseAltText,
		// The grid's three axes — a name, a media family, and the stamp the ledger leaves
		// when the last use ends — are decided in contracts so the SQL and the fake cannot
		// answer them differently. The family in particular: a filter that trusted the type
		// prefix alone would let a drawing nobody measured pose as a photograph. An unknown
		// value is refused naming what it accepts, because a grid that showed everything
		// because a filter was misspelled would show a person the files they asked not to.
		"the grid filters by name, by family and by what nothing shows": filterLibrary,
		// A file the ledger still shows is not deletable, and the refusal is the module's
		// rather than a control's: a library that dims a button is a courtesy, and this is
		// the answer a client that never saw the page gets. Ending the last use is what
		// makes the removal possible — the same rewrite that stamps the grace period the
		// release sweep measures. The removals that outrank a record's convenience are a
		// subject's erasure and the two sweeps, none of which come through here.
		"a file another record shows is not removed": refuseUsedFile,
		// Records, not rows: one page that shows one picture in its body and its summary is
		// one page to go and edit, and contracts.DistinctRecords is the count both
		// implementations give. Counting rows would answer 4 about a refusal a person can
		// only act on twice.
		"a file two records show names two records": listUsingRecords,
		// The audience a record asserts travels into the rewrite that files its uses, so a
		// published page that gains a picture gains public bytes in the write that added
		// it. SetShown stays the only door that changes the audience of a row it did not
		// write.
		"an added use of a record with a public audience is public at once": publishAddedUse,
		// The same rewrite with no audience asserts nothing: a private file stays private,
		// and a file somebody raised by hand keeps what they asked for — the recompute of
		// added files runs only under a public audience, so a draft never takes a
		// hand-raised visibility back.
		"an added use of a record with no audience flips nothing": keepUnshownUsePrivate,
		// The retract this module already had, named as a case so the audience path above
		// cannot quietly drop it: the last public use ending is what turns a file private
		// again, in the write that ended it.
		"the last public use ended makes a file private again":    privatiseLastPublicUse,
		"deleting removes the row and says where the bytes are":   deleteFile,
		"an unknown id is not found":                              refuseUnknownFile,
		"a file needs a name and a visibility that is one of two": validateFile,
	}
}
