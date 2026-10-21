package designconfig_test

// Review round 1 (T-0107). designconfig.LoadClientDesigns is the door a process
// boots its client identities through, and designconfig.go:19-23 states why the
// door exists at all: "a copy of another client's file is exactly the mistake a
// directory tree invites, and the pair a process ends up wearing is not
// something to discover in an incident." The loader decides which directories to
// read with fs.Stat and then reads them with fs.ReadFile. Those two calls do not
// agree on every fs.FS: when stat succeeds and the read fails, the read error
// returned by LoadClientDesign is never reached, the loop continues, and the
// client is missing from a returned set that reports no error at all. The
// composition then wears design.Default() for that client — the quiet substitution
// the package doc refuses to accept. The case asserts the correct behaviour, so
// it passes the day the loader surfaces the read failure.

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/designconfig"
)

// soundClient is a design.yaml that resolves: a seed and nothing else.
func soundClient(slug, sector, name string) string {
	return "slug: " + slug + "\nseed:\n  sector: " + sector + "\n  name: " + name + "\n"
}

// TestLoadClientDesignsRefusesAFileItCannotRead puts three client directories on
// disk, one of them with a design.yaml whose bytes are there but whose mode
// refuses the read, and asks what the loader returns for the set.
func TestLoadClientDesignsRefusesAFileItCannotRead(t *testing.T) {
	const tree = "clients-unreadable"
	mapfs := fstest.MapFS{
		tree + "/amber/" + designconfig.ClientDesignFile:  {Mode: 0o444, Data: []byte(soundClient("amber", "energy", "Amber"))},
		tree + "/beacon/" + designconfig.ClientDesignFile: {Mode: 0o444, Data: []byte(soundClient("beacon", "public", "Beacon"))},
		tree + "/broken/" + designconfig.ClientDesignFile: {Mode: 0o000, Data: []byte(soundClient("broken", "legal", "Broken"))},
	}
	if _, err := fs.Stat(mapfs, tree+"/broken/"+designconfig.ClientDesignFile); err != nil {
		t.Fatalf("the fixture must be visible to fs.Stat for this case to mean anything: %v", err)
	}
	if _, err := fs.ReadFile(mapfs, tree+"/broken/"+designconfig.ClientDesignFile); err == nil {
		t.Skip("this fs.FS serves a 0000-mode file, so it cannot model a read that fails after a stat succeeds")
	}
	if _, err := designconfig.LoadClientDesign(mapfs, tree+"/broken"); err == nil {
		t.Fatal("reading the unreadable design.yaml directly must refuse")
	}

	pairs, err := designconfig.LoadClientDesigns(mustSub(t, mapfs, tree))
	if err == nil {
		t.Errorf("%d client directories hold a design.yaml; LoadClientDesigns returned %d pairs and no error: the client whose file could not be read is absent from a set that claims to be complete",
			3, len(pairs))
	}
}

// mustSub roots an fs.FS at a directory, so LoadClientDesigns sees exactly the
// client directories this case laid out.
func mustSub(t *testing.T, fsys fstest.MapFS, dir string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}
