package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Static mounts a module-relative file tree under the surface's outer headers,
// without tenant transactions or response buffering. It records the mount and
// collects refusals for Ops trees, namespace roots and invalid relative paths.
// Misses use the surface's negotiated response; see the surface guide in README.md.
func (r *Router) Static(rel string, fsys fs.FS) {
	if fault := r.staticFault(rel); fault != "" {
		r.api.noteRefusal(fault)
		// And nothing is mounted: the composition never listens because the gate
		// that reads the refusal runs before anything opens a port, and a tree
		// mounted while it was refused would be a door the audit does not list.
		return
	}
	at := strings.TrimSuffix(r.compose(rel, true), "/")
	r.api.noteMount(mounted{
		surface: r.surface, module: r.module, method: http.MethodGet, path: at + "/*",
		// A file tree asks nothing of anybody, which is the question Public()
		// answers. It is no operation and declares nothing of its own; the boot
		// line prints it beside the workspace doors that admit an anonymous
		// caller, because that is the honest sentence about a stylesheet: whoever
		// reaches the address is served it, with the surface's own headers.
		auth: Public(), page: true, static: true,
	})
	r.api.root.Handle(at+"/*", http.StripPrefix(at, r.api.tree(fsys)))
}

// assetCaching is the static-asset row of docs/cache.md, for one file of a kernel tree.
//
// A kernel asset is the same bytes for every person and every tenant of a process,
// so the session rule does not apply to it, and the surface's own answer was the
// wrong one twice over: the workspace sent its stylesheet and its controllers
// no-store, so every page view downloaded them again, and the public face let a
// cache keep them for a minute with nothing to revalidate against. Now every file
// carries an ETag that is its content — the same first eight bytes of SHA-256 that
// ui.Sheet puts in the stylesheet's `?v=` — so a revalidation that finds it unchanged
// is a 304 (net/http answers If-None-Match from this header), and a request whose
// `?v=` names exactly those bytes may be kept for a year, immutable, because a
// different body would have a different address. The hash is taken per request:
// the files are small and a tree may be a directory that changes under a developer.
func assetCaching(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return // the surface's own answer stands; the file server reports the error
	}
	sum := sha256.Sum256(body)
	tag := hex.EncodeToString(sum[:8])
	h := w.Header()
	h.Set("ETag", `"`+tag+`"`)
	if r.URL.Query().Get("v") == tag {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	for writer := w; writer != nil; {
		if marker, ok := writer.(interface{ markStatic() }); ok {
			marker.markStatic()
			break
		}
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		writer = unwrapper.Unwrap()
	}
}

// tree serves existing files unchanged, including range and conditional requests.
// Missing files and directories use the surface's negotiated refusal instead of
// net/http errors or directory listings.
func (a *API) tree(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cleaned rather than raw: fs.Stat refuses a path carrying a .., and the
		// refusal that describes a file nobody has is the one for a file that is
		// simply not there.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if !treeHolds(fsys, name) {
			a.fail(w, r, http.StatusNotFound, "nothing is served at this address")
			return
		}
		assetCaching(w, r, fsys, name)
		files.ServeHTTP(w, r)
	})
}

// treeHolds reports whether this tree has a file at that name. A directory is not
// an address anybody asked for, and the mount prefix is the same case with the name
// empty; no mounted tree here carries an index for either, so refusing them loses
// nothing and forecloses the listing.
func treeHolds(fsys fs.FS, name string) bool {
	if name == "" {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

// staticFault is the two refusals the prefix rules do not already make: which
// surfaces have documents at all, and who owns a namespace's root.
func (r *Router) staticFault(rel string) string {
	if r.surface == SurfaceOps {
		return fmt.Sprintf("httpx: module %q: Static(%q) on the Ops router would publish a file tree at the document address the control plane does not have; the Ops surface is JSON and is served at the installation host only, and the address it composes for a page is empty, so the tree would answer at every customer's host under a public-looking prefix — mount it on the App router, whose chrome the operator stands in front of", r.module, rel)
	}
	if strings.Trim(rel, "/") == "" {
		return fmt.Sprintf("httpx: module %q: Static(%q) on the %s router mounts a file tree at the root of %s, which is the namespace the routes of %s answer in; a tree is mounted beneath a namespace and not at its root, because chi would let the file server answer for everything beneath it — write Static(\"/assets\", tree)", r.module, rel, r.surface, r.pagePrefix(), r.pagePrefix())
	}
	return ""
}
