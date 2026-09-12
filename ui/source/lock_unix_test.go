//go:build unix && !aix

package source

import "testing"

func TestModuleLockSerializesCooperatingWriters(t *testing.T) {
	root := t.TempDir()
	unlock, err := lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := lock(root); err == nil {
		release()
		unlock()
		t.Fatal("a second writer acquired the same module")
	}
	other, err := lock(t.TempDir())
	if err != nil {
		unlock()
		t.Fatal("independent modules cannot be edited concurrently:", err)
	}
	other()
	unlock()
	released, err := lock(root)
	if err != nil {
		t.Fatal("released module remains locked:", err)
	}
	released()
}
