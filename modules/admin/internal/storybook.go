package internal

import (
	"context"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/export"
)

// Storybook's entire build is private. Never mount it with API.Static: even
// index.json and hashed chunks disclose the selected examples and properties.
func (p pages) mountStorybook(app *httpx.Router) {
	op := huma.Operation{OperationID: "admin-storybook-file", Method: http.MethodGet,
		Path: p.at.gallery.rel + "/storybook/*", Tags: []string{"admin"}, Hidden: true}
	httpx.SignIn(&op, p.at.login.at)
	httpx.HTML(app, op, httpx.Permission("gallery:read"), func(ctx context.Context, in *struct {
		File string `path:"*" maxLength:"500"`
	}) (*httpx.Page, error) {
		book, err := p.storybook(ctx)
		if err != nil {
			return nil, err
		}
		name := in.File
		if book.Files == nil || !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
			return nil, problem.New(http.StatusNotFound, "Storybook file not available.")
		}
		// An accidentally shared or stale build must fail closed, including when
		// tenants share IDs but differ in props, slots, private CSS or theme.
		var manifest struct {
			SHA256 string `json:"sha256"`
		}
		data, err := fs.ReadFile(book.Files, "platformkit.json")
		if err != nil || json.Unmarshal(data, &manifest) != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "Rebuild Storybook for this composition.")
		}
		snapshot, err := export.Export(book.Theme, book.Examples, book.Extra...)
		if err != nil {
			return nil, err
		}
		if manifest.SHA256 != snapshot.SHA256 {
			return nil, problem.New(http.StatusServiceUnavailable, "Rebuild Storybook for this composition.")
		}
		body, err := fs.ReadFile(book.Files, name)
		if err != nil {
			return nil, problem.New(http.StatusNotFound, "Storybook file not available.")
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return &httpx.Page{Status: http.StatusOK, ContentType: contentType, CacheControl: "no-store", FrameOptions: "SAMEORIGIN",
			// Storybook's generated bootstrap uses inline scripts. This policy is
			// confined to its trusted build; Go specimen documents stay sandboxed.
			ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'none'",
			Body:                  body}, nil
	})
}
