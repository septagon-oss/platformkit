package file_test

import (
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"testing"
)

func TestLocalStorageConforms(t *testing.T) {
	filetest.RunStorage(t, func(t *testing.T) filetest.StorageFixture {
		return filetest.StorageFixture{Storage: file.Local(t.TempDir())}
	})
}
