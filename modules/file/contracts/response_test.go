package contracts_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

type seekableBody struct {
	*strings.Reader
	closed bool
}

func (s *seekableBody) Close() error { s.closed = true; return nil }

func TestContentResponsePreservesHTTPAndClosesEveryBody(t *testing.T) {
	for _, tc := range []struct {
		name, method, byteRange, wantBody string
		status                            int
		seekable, attachment              bool
	}{
		{"whole", "GET", "", "0123456789", 200, true, false},
		{"range", "GET", "bytes=2-5", "2345", 206, true, false},
		{"suffix", "GET", "bytes=-2", "89", 206, true, false},
		{"head", "HEAD", "", "", 200, true, false},
		{"head range", "HEAD", "bytes=2-5", "", 206, true, false},
		{"invalid range", "GET", "bytes=99-100", "invalid range: failed to overlap\n", 416, true, false},
		{"stream", "GET", "", "0123456789", 200, false, false},
		{"stream head", "HEAD", "", "", 200, false, false},
		{"stream range fallback", "GET", "bytes=2-5", "0123456789", 200, false, false},
		{"attachment", "GET", "bytes=2-5", "2345", 206, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://academy.test/media", nil)
			r.Header.Set("Range", tc.byteRange)
			stored := &contracts.File{Name: "lesson.mp4", ContentType: "video/mp4", Size: 10, Visibility: contracts.VisibilityPrivate}
			stored.UpdatedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			body := &seekableBody{Reader: strings.NewReader("0123456789")}
			var input io.ReadCloser = body
			if !tc.seekable {
				input = struct{ io.ReadCloser }{body}
			}
			mode := contracts.Inline
			if tc.attachment {
				mode = contracts.Attachment
			}
			response := contracts.ContentResponse(r, stored, input, mode)
			w := httptest.NewRecorder()
			ctx := humachi.NewContext(&huma.Operation{}, r, w)
			response.Body(ctx)
			if w.Code != tc.status || ctx.Status() != tc.status || w.Body.String() != tc.wantBody || !body.closed {
				t.Fatalf("wire=%d transaction=%d body=%q closed=%v", w.Code, ctx.Status(), w.Body.String(), body.closed)
			}
			for name, want := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'; sandbox", "Cross-Origin-Resource-Policy": "same-site"} {
				if got := w.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if !strings.HasPrefix(w.Header().Get("Content-Disposition"), string(mode)) {
				t.Fatal("wrong disposition")
			}
		})
	}
}

func TestContentResponseNeverRendersActiveDocumentsInline(t *testing.T) {
	r := httptest.NewRequest("GET", "http://academy.test/media", nil)
	w := httptest.NewRecorder()
	response := contracts.ContentResponse(r, &contracts.File{Name: "x.html", ContentType: "text/html", Visibility: contracts.VisibilityPrivate, Size: 4}, io.NopCloser(strings.NewReader("evil")), contracts.Inline)
	response.Body(humachi.NewContext(&huma.Operation{}, r, w))
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("caller bypassed the inline allowlist")
	}
}
