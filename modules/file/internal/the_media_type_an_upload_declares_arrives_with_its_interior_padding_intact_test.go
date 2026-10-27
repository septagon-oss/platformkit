package internal

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTheMediaTypeAnUploadDeclaresArrivesWithItsInteriorPaddingIntact pins which
// declared media types can reach the content_type column at all.
//
// The library's three type filters read that column through an SQL expression and
// its own rows through contracts.Essence, and a case that asks the two readings
// the same question can only answer for the values the column is able to hold.
// This says which those are: the upload door hands over the part's declared type
// as its header carries it, so the padding between a type and its parameters and
// the casing of either reach the row, while whitespace at the ends of the whole
// value does not — the header reader removes it before the door sees it. A door
// that took the media type from a field, a query parameter or a value it padded
// itself would move which forms the two readings have to agree over, and this
// refuses it rather than leaving the agreement's coverage to a guess.
//
// It reads through the same door the upload handler reads through — the multipart
// reader net/http puts on the request — over bodies written byte for byte the way
// a client writes them, so nothing here rests on a writer's own escaping.
func TestTheMediaTypeAnUploadDeclaresArrivesWithItsInteriorPaddingIntact(t *testing.T) {
	const boundary = "pkit-door-boundary"
	declared := []struct{ says, arrives string }{
		{says: "image/jpeg", arrives: "image/jpeg"},
		{says: "Image/JPEG", arrives: "Image/JPEG"},
		{says: "image/jpeg;charset=utf-8", arrives: "image/jpeg;charset=utf-8"},
		{says: "image/jpeg ;charset=utf-8", arrives: "image/jpeg ;charset=utf-8"},
		{says: "image/jpeg\t;charset=utf-8", arrives: "image/jpeg\t;charset=utf-8"},
		{says: " VIDEO/MP4 ;type=streaming ", arrives: "VIDEO/MP4 ;type=streaming"},
		{says: "\tvideo/mp4", arrives: "video/mp4"},
		{says: " video/mp4\t", arrives: "video/mp4"},
	}
	for _, one := range declared {
		body := "--" + boundary + "\r\n" +
			"Content-Disposition: form-data; name=\"file\"; filename=\"a.bin\"\r\n" +
			"Content-Type: " + one.says + "\r\n\r\n" +
			"the bytes\r\n--" + boundary + "--\r\n"
		req := httptest.NewRequest(http.MethodPost, "/api/v1/files", strings.NewReader(body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

		parts, err := req.MultipartReader()
		if err != nil {
			t.Fatalf("the door would not read a form declaring %q: %v", one.says, err)
		}
		part, err := parts.NextPart()
		if err != nil {
			t.Fatalf("the first part of a form declaring %q: %v", one.says, err)
		}
		if part.FileName() == "" {
			t.Fatalf("the door took the file part of a form declaring %q for a field", one.says)
		}
		if got := part.Header.Get("Content-Type"); got != one.arrives {
			t.Errorf("a form declaring %q arrives as %q: the column holds what the door is handed", one.says, got)
		}
		if _, err := io.Copy(io.Discard, part); err != nil {
			t.Fatalf("reading the part that declares %q: %v", one.says, err)
		}
		if err := part.Close(); err != nil {
			t.Fatalf("closing the part that declares %q: %v", one.says, err)
		}
	}
}
