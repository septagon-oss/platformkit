package internal

import (
	"runtime"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

func TestFileServicesShareTheProcessDecodeBound(t *testing.T) {
	first := NewService(filetest.NewMemory(), 1<<20, 0, 0)
	second := NewService(filetest.NewMemory(), 1<<20, 0, 0)
	for range runtime.GOMAXPROCS(0) {
		release, ok := first.decode.wait(t.Context(), time.Second)
		if !ok {
			t.Fatal("the process bound refused a slot before GOMAXPROCS frames were held")
		}
		t.Cleanup(release)
	}
	if release, ok := second.decode.wait(t.Context(), 10*time.Millisecond); ok {
		release()
		t.Fatal("a second file service admitted a frame after the process held GOMAXPROCS frames")
	}
}
