package file_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// A width is served at the address its srcset candidate names, and nothing else
// is served there. Three things had no test at the door before this file, and each
// is one of the ways the delivery could have been wrong while every service-level
// case still passed:
//
//   - a width outside the served set is refused by name (422) rather than answered
//     with the nearest one, and the refusal happens on both doors;
//   - a width the plan never built is the same 404 a stranger gets, and never a
//     quiet fallback to the original, which would make one address two objects;
//   - a built width's response describes the object it sends: the Content-Length
//     of a body that cannot seek is the width's own size, not the frame's, and the
//     bytes are the 480 px picture rather than the 1000 px one.

// widthsMount is the module over a real Postgres, one directory and a tenant the
// encode pass can walk, with the upload ceiling a photograph needs. store is the
// caller's, because one case wants a store whose Get cannot seek.
func widthsMount(t *testing.T, store contracts.Storage) (chi.Router, *db.Conn, module.Module) {
	t.Helper()
	_, conn := dbtest.Schema(t, file.Migrations)
	hosts := map[string]tenancy.Tenant{host: acme}
	api, router := httpx.New(httpx.Options{
		SystemAccess: declaredCrossTenantAccess(),
		Cache:        cache.Memory("pkit"), PublicHost: host, Tenants: caller{hosts: hosts},
		Conn: conn, Authorize: caller{hosts: hosts},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := file.New(file.Deps{Storage: store, MaxBytes: 16 << 20,
		Tenants: widthsOnly{walking: []tenancy.Tenant{acme}}, VariantEvery: time.Second})
	m.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, conn, m
}

// widthsOnly is the lister the encode pass walks: one tenant, named here rather
// than read from a tenants table this mount resolves origins out of a map for.
type widthsOnly struct{ walking []tenancy.Tenant }

func (w widthsOnly) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return w.walking, nil
}

// encodeNow runs the pass that builds the widths, by name: the job is the only way
// a pending plan becomes a ready width, and the door serves what the row says.
func encodeNow(t *testing.T, m module.Module, conn *db.Conn) {
	t.Helper()
	for _, j := range m.Jobs {
		if j.Name == "file-variants" {
			if err := j.Run(t.Context(), conn); err != nil {
				t.Fatalf("the encode pass: %v", err)
			}
			return
		}
	}
	t.Fatal("the module mounts no encode pass, so no width can become ready")
}

// wideFrame is a 1000x500 JPEG: bigger than every width this deployment serves, so
// the plan it files is 480, 960 and 1440, and it is real pixels rather than a
// declared size, because the pass measures the frame and the plan follows the frame.
func wideFrame(t *testing.T) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for x := range 1000 {
		for y := range 500 {
			frame.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	out := &bytes.Buffer{}
	if err := jpeg.Encode(out, frame, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode the frame: %v", err)
	}
	return out.Bytes()
}

// uploadFrame posts that frame and answers the row's id and its size in bytes.
func uploadFrame(t *testing.T, router chi.Router, at string) (string, int64) {
	t.Helper()
	picture := wideFrame(t)
	code, out := upload(t, router, at, "wide.jpg", "image/jpeg", string(picture))
	if code != http.StatusCreated {
		t.Fatalf("POST a 1000 px frame = %d %s", code, out)
	}
	var row struct {
		ID   uuid.UUID `json:"id"`
		Size int64     `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &row); err != nil || row.ID == uuid.Nil {
		t.Fatalf("the uploaded row does not say who it is: %v %s", err, out)
	}
	if row.Size <= 0 {
		t.Fatalf("the row's size = %d, want the bytes the pass stored (it re-encodes the frame, so not the %d that arrived)",
			row.Size, len(picture))
	}
	return row.ID.String(), row.Size
}

func TestTheWidthDoorRefusesAWidthItDoesNotServe(t *testing.T) {
	router, _, _ := widthsMount(t, file.Local(t.TempDir()))
	id, _ := uploadFrame(t, router, files)
	code, out := upload(t, router, files+"?visibility=public", "wide.jpg", "image/jpeg", string(wideFrame(t)))
	if code != http.StatusCreated {
		t.Fatalf("POST a public frame = %d %s", code, out)
	}
	open := field(t, out, "id")

	// No encode pass has run, so nothing is built: every planned width is a 404 and
	// every other number is a refusal that says which numbers exist.
	for _, tc := range []struct {
		at   string
		sign bool
		want int
	}{
		{files + "/" + id + "/content?w=700", true, http.StatusUnprocessableEntity},
		{files + "/" + id + "/content?w=0", true, http.StatusOK},
		{files + "/" + id + "/content?w=480", true, http.StatusNotFound},
		{files + "/" + id + "/content?w=1920", true, http.StatusNotFound},
		{public + open + "?w=700", false, http.StatusUnprocessableEntity},
		{public + open + "?w=480", false, http.StatusNotFound},
		// A private file's width at the open door is the same 404 its bare
		// address is: the width changes the object, not the authorisation.
		{public + id + "?w=480", false, http.StatusNotFound},
		// The signed-in door is still a door only a session opens.
		{files + "/" + id + "/content?w=480", false, http.StatusUnauthorized},
	} {
		code, body, _ := send(t, router, http.MethodGet, tc.at, tc.sign)
		if code != tc.want {
			t.Errorf("GET %s signed in=%v = %d %q, want %d", tc.at, tc.sign, code, body, tc.want)
		}
		if tc.want == http.StatusUnprocessableEntity {
			for _, w := range contracts.VariantWidthSet {
				if !strings.Contains(body, strconv.Itoa(w)) {
					t.Errorf("the refusal of %s does not name %d: %q", tc.at, w, body)
				}
			}
			if !strings.Contains(body, "700") {
				t.Errorf("the refusal of %s does not name the width it refused: %q", tc.at, body)
			}
		}
		// The body of a refusal is never the picture: a fallback to the frame is
		// the one thing this door may not do.
		if tc.want == http.StatusNotFound {
			if img, _, err := image.DecodeConfig(strings.NewReader(body)); err == nil {
				t.Errorf("GET %s answered a %dx%d picture where a 404 belongs", tc.at, img.Width, img.Height)
			}
		}
	}
	// The bare address is untouched by all of this: it serves the frame it always
	// served, at the frame's own size.
	code, body, header := send(t, router, http.MethodGet, files+"/"+id+"/content", true)
	if code != http.StatusOK || header.Get("Content-Length") == "" {
		t.Fatalf("GET the bare address = %d, want the frame", code)
	}
	img, _, err := image.DecodeConfig(strings.NewReader(body))
	if err != nil || img.Width != 1000 {
		t.Errorf("the bare address answered a %d px frame, want the stored 1000 px one: %v", img.Width, err)
	}
}

func TestTheWidthDoorServesTheWidthItsRowCallsReady(t *testing.T) {
	dir := t.TempDir()
	router, conn, m := widthsMount(t, streamed{file.Local(dir)})
	id, frame := uploadFrame(t, router, files)
	encodeNow(t, m, conn)

	code, body, header := send(t, router, http.MethodGet, files+"/"+id+"/content?w=480", true)
	if code != http.StatusOK {
		t.Fatalf("GET the built 480 px width = %d %q, want 200", code, body)
	}
	img, _, err := image.DecodeConfig(strings.NewReader(body))
	if err != nil || img.Width != 480 {
		t.Fatalf("the built width is a %d px picture (%v), want 480", img.Width, err)
	}
	// The store this case mounts answers Get with a body that cannot seek, which
	// is the branch that writes Content-Length from the row rather than from the
	// object — and so the branch that said the frame's size while sending a width.
	if length := header.Get("Content-Length"); length != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want the %d bytes actually sent", length, len(body))
	}
	if length := header.Get("Content-Length"); length == strconv.FormatInt(frame, 10) {
		t.Errorf("Content-Length = the frame's %d bytes, not the width's", frame)
	}
	// The frame is still there, still served, still its own size.
	code, body, header = send(t, router, http.MethodGet, files+"/"+id+"/content", true)
	if code != http.StatusOK || header.Get("Content-Length") != strconv.FormatInt(frame, 10) {
		t.Errorf("GET the bare address after an encode = %d with Content-Length %q, want the frame's %d bytes",
			code, header.Get("Content-Length"), frame)
	}
}

// streamed is a store whose Get returns a body that reads and closes and cannot
// seek: what an adapter that streams from somewhere else does (contracts.Storage
// promises no io.Seeker), and therefore the only way in this repository to reach
// the half of contracts.ContentResponse that writes a declared length itself.
type streamed struct{ contracts.Storage }

func (s streamed) Get(ctx context.Context, scope contracts.Scope, key contracts.Key) (io.ReadCloser, error) {
	body, err := s.Storage.Get(ctx, scope, key)
	if err != nil {
		return nil, err
	}
	return unreadable{body}, nil
}

// unreadable keeps Read and Close and loses everything else, which is the point:
// an embedded io.ReadCloser would still answer a type assertion for io.ReadSeeker.
type unreadable struct{ body io.ReadCloser }

func (u unreadable) Read(p []byte) (int, error) { return u.body.Read(p) }
func (u unreadable) Close() error               { return u.body.Close() }
