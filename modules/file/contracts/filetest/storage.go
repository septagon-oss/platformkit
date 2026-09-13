package filetest

import (
	"github.com/septagon-oss/platformkit/kit/blob/blobtest"
	"testing"
)

// StorageFixture is the standalone storage provider fixture.
type StorageFixture = blobtest.StorageFixture

// RunStorage forwards to the one byte contract suite. New standalone provider
// tests import blobtest directly to avoid File's service-test dependencies.
func RunStorage(t *testing.T, fresh func(*testing.T) StorageFixture) {
	t.Helper()
	blobtest.RunStorage(t, fresh)
}
