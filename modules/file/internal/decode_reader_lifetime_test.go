package internal

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

type decodeReaderStore struct {
	contracts.Storage
	opened int
	closed int
}

func (s *decodeReaderStore) Get(ctx context.Context, scope contracts.Scope, key contracts.Key) (io.ReadCloser, error) {
	body, err := s.Storage.Get(ctx, scope, key)
	if err != nil {
		return nil, err
	}
	s.opened++
	return &decodeReader{ReadCloser: body, closed: &s.closed}, nil
}

type decodeReader struct {
	io.ReadCloser
	closed *int
}

func (r *decodeReader) Close() error {
	*r.closed++
	return r.ReadCloser.Close()
}

func TestARefusedDecodeClosesItsStoredImageReader(t *testing.T) {
	store := &decodeReaderStore{Storage: filetest.NewMemory()}
	scope := filetest.TenantScope(t, uuid.New())
	key := contracts.Key(uuid.NewString())
	if err := store.Put(t.Context(), scope, key, strings.NewReader("image bytes"), -1, contracts.Meta{}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, 1<<20, 0, 0)
	svc.decode = newDecodeFrames(1)
	release, ok := svc.decode.wait(t.Context(), time.Second)
	if !ok {
		t.Fatal("could not occupy the decode slot")
	}
	defer release()
	pass, err := svc.reencode(t.Context(), scope, contracts.Upload{Image: true}, key, nil)
	if pass != nil || !errors.Is(err, contracts.ErrBusyDecoding) {
		t.Fatalf("saturated decode = %v, %v; want no frame and ErrBusyDecoding", pass, err)
	}
	if store.opened != store.closed {
		t.Fatalf("saturated decode opened %d stored readers and closed %d; every opened reader must close", store.opened, store.closed)
	}
}
