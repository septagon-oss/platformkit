//go:build !unix

package wire_test

import "testing"

// Other platforms use os.Chmod's read-only attribute without a Unix root UID.
func goldenWriterWithoutRoot(t *testing.T) { t.Helper() }
