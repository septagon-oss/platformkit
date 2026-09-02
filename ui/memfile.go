package ui

import (
	"bytes"
	"io"
	"io/fs"
	"time"
)

// memFile is one in-memory file, enough for net/http's file server: it reads,
// it seeks (so a range request works), and it can describe itself.
type memFile struct {
	name string
	body []byte
	r    *bytes.Reader
}

func (f *memFile) reader() *bytes.Reader {
	if f.r == nil {
		f.r = bytes.NewReader(f.body)
	}
	return f.r
}

func (f *memFile) Read(p []byte) (int, error)                { return f.reader().Read(p) }
func (f *memFile) Seek(off int64, whence int) (int64, error) { return f.reader().Seek(off, whence) }
func (f *memFile) Close() error                              { return nil }
func (f *memFile) Stat() (fs.FileInfo, error)                { return f, nil }
func (f *memFile) Name() string                              { return f.name }
func (f *memFile) Size() int64                               { return int64(len(f.body)) }
func (f *memFile) Mode() fs.FileMode                         { return 0o444 }
func (f *memFile) ModTime() time.Time                        { return time.Time{} }
func (f *memFile) IsDir() bool                               { return false }
func (f *memFile) Sys() any                                  { return nil }

var _ io.ReadSeeker = (*memFile)(nil)
