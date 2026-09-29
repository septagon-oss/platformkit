package designconfig_test

// Review round 7 (T-0107). Round 1's file in this directory asks what the loader
// does when fs.Stat succeeds and fs.ReadFile fails on a client's design.yaml, and
// it skips: the only fixture it has is a testing/fstest.MapFS, and MapFS serves a
// 0000-mode file to anyone who asks, so the read never fails, the premise never
// holds, and the case has not run in any environment — not this one, not CI. The
// behaviour it asks for is the one the package doc promises ("Nothing partial is
// returned … including a directory whose file could not be read, which fails the
// load rather than going unmentioned"), and
// TestLoadClientDesignsRefusesADirectoryItCannotRead covers only os.DirFS, where
// mode 0000 does fail a read.
//
// This case reaches the same branch through a door that works everywhere: a
// composition decides whether the tree is a disk, an embed.FS or a fixture, so an
// fs.FS that serves a directory listing and then refuses one file is a legal
// collaborator, not a contrivance — it is what a mounted volume mid-unmount, or a
// permission change between the scan and the read, looks like from here. It
// asserts the correct behaviour, so it passes today and refuses the day a caller
// can make the loader skip a client it cannot read.

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/designconfig"
)

// r7SoundClient is a design.yaml that resolves: a seed and nothing else. Owned by
// this file so the case does not depend on a helper another file may rename.
func r7SoundClient(slug, sector, name string) string {
	return "slug: " + slug + "\nseed:\n  sector: " + sector + "\n  name: " + name + "\n"
}

// errFS serves a listing of clients and then refuses to hand over one file, the
// way a volume that went away between the scan and the read does.
type errFS struct {
	fsys   fs.FS
	refuse string // path to refuse, relative to this root
	err    error
}

func (e errFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(e.fsys, name) }

func (e errFS) ReadFile(name string) ([]byte, error) {
	if name == e.refuse {
		return nil, e.err
	}
	return fs.ReadFile(e.fsys, name)
}

func (e errFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(e.fsys, name) }

func (e errFS) Open(name string) (fs.File, error) { return e.fsys.(fs.FS).Open(name) }

// TestLoadClientDesignsRefusesAClientFileThatErrors checks that a design.yaml the
// filesystem refuses for a reason other than "it is not there" fails the whole
// load: no set is returned, and the client is not quietly left out to be worn as
// design.Default().
func TestLoadClientDesignsRefusesAClientFileThatErrors(t *testing.T) {
	t.Parallel()
	const tree = "clients"
	mapfs := fstest.MapFS{
		tree + "/amber/" + designconfig.ClientDesignFile:  {Mode: 0o444, Data: []byte(r7SoundClient("amber", "energy", "Amber"))},
		tree + "/beacon/" + designconfig.ClientDesignFile: {Mode: 0o444, Data: []byte(r7SoundClient("beacon", "public", "Beacon"))},
		tree + "/broken/" + designconfig.ClientDesignFile: {Mode: 0o444, Data: []byte(r7SoundClient("broken", "legal", "Broken"))},
	}
	sub, err := fs.Sub(mapfs, tree)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("device or resource busy")
	refusing := errFS{fsys: sub, refuse: "broken/" + designconfig.ClientDesignFile, err: sentinel}

	// The premise, checked before the claim: the directory is listed and the file
	// is there to stat, and only the read of that one file fails. A case whose
	// refusal was really an absent file would prove nothing.
	entries, err := fs.ReadDir(refusing, ".")
	if err != nil || len(entries) != 3 {
		t.Fatalf("the scan must see three client directories, saw %d (%v)", len(entries), err)
	}
	if _, err := fs.Stat(refusing, "broken/"+designconfig.ClientDesignFile); err != nil {
		t.Fatalf("the fixture must be visible to fs.Stat for this case to mean anything: %v", err)
	}
	if _, err := fs.ReadFile(refusing, "broken/"+designconfig.ClientDesignFile); !errors.Is(err, sentinel) {
		t.Fatalf("the fixture must refuse exactly that read, got %v", err)
	}

	pairs, err := designconfig.LoadClientDesigns(refusing)
	if err == nil {
		t.Errorf("%d client directories hold a design.yaml; the load returned %d pairs and no error: the client whose file the filesystem refused is absent from a set that claims to be complete",
			3, len(pairs))
		return
	}
	if pairs != nil {
		t.Errorf("a refused load returned %d pairs (%v), want none: a refused set is not a partial set", len(pairs), pairs)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("the refusal hides what the filesystem said: %v", err)
	}
}
