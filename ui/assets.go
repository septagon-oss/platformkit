package ui

import (
	"io/fs"
	"slices"
)

// Assets is the tree a shell serves under its asset prefix: app.css is the
// sheet it composed, gallery.css is Gallery, js/ is Controllers, and then any
// trees the consumer adds — its own scripts — looked up in order after the
// computed files and before the kernel's.
func Assets(app Sheet, more ...fs.FS) fs.FS {
	js, err := fs.Sub(scripts, "assets")
	if err != nil {
		panic("ui: the embedded scripts are not where they were embedded: " + err.Error())
	}
	return overlay{
		files:  map[string][]byte{"app.css": app.Body, "gallery.css": Gallery().Body},
		layers: append(slices.Clone(more), js),
	}
}

func (o overlay) Open(name string) (fs.File, error) {
	if body, ok := o.files[name]; ok {
		return &memFile{name: name, body: body}, nil
	}
	for _, layer := range o.layers {
		if f, err := layer.Open(name); err == nil {
			return f, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// overlay is computed files in front of a stack of trees: the sheets, which
// have no source to embed because they are composed, then whatever a consumer
// adds, then the kernel's scripts. It is a dozen lines rather than a build step
// that writes files the repository would then have to ignore.
type overlay struct {
	files  map[string][]byte
	layers []fs.FS
}
