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
		"an upload is counted and hashed as it goes past":                         countAndHashUpload,
		"the same bytes twice are two files":                                      separateRepeatedUploads,
		"an upload past the limit keeps nothing":                                  refuseOversizeUpload,
		"an upload of exactly the limit is kept":                                  acceptExactLimitUpload,
		"a private file is not found at the public door":                          concealPrivateFile,
		"a public file is served to anybody":                                      servePublicFile,
		"an upload whose bytes disagree with its type is refused":                 refuseMIMEMismatch,
		"an image is measured and stored as the pixels it re-encodes to":          reencodeImage,
		"what no decoder reads is refused at the image door and keeps nothing":    refuseUndecodableImage,
		"a record's uses are its body's references, rewritten in place":           replaceRecordUses,
		"the uses of a file that is not there are refused and nothing is written": refuseMissingUse,
		"a use ended of a file that is already gone ends":                         endUseOfDeletedFile,
		"an alt text is written against the row it was shown":                     reviseAltText,
		"the grid filters by name, by family and by what nothing shows":           filterLibrary,
		"a file another record shows is not removed":                              refuseUsedFile,
		"a file two records show names two records":                               listUsingRecords,
		"an added use of a record with a public audience is public at once":       publishAddedUse,
		"an added use of a record with no audience flips nothing":                 keepUnshownUsePrivate,
		"the last public use ended makes a file private again":                    privatiseLastPublicUse,
		"deleting removes the row and says where the bytes are":                   deleteFile,
		"an unknown id is not found":                                              refuseUnknownFile,
		"a file needs a name and a visibility that is one of two":                 validateFile,
	}
}
