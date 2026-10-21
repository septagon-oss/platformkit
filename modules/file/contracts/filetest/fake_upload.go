package filetest

import (
	"context"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// uploadImage reads the stored stream before opening a transaction. A refused
// image pass removes the original object; an optional failed pass retains it.
func (f *Fake) uploadImage(ctx context.Context, scope contracts.Scope, key contracts.Key, up contracts.Upload, head []byte) (*contracts.ImagePass, error) {
	body, err := f.storage.Get(ctx, scope, key)
	if err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, err
	}
	var pass *contracts.ImagePass
	if contracts.ReadsAsImage(up, head) {
		got, perr := contracts.ProcessImage(body, contracts.DefaultMaxImagePixels)
		if perr != nil {
			err = perr
		} else {
			pass = &got
		}
	}
	_ = body.Close()
	if err != nil {
		// The same rule as the service: a pass that failed refuses and removes
		// the object only when the caller called it an image or asked for more
		// pixels than this deployment decodes.
		if contracts.RefusesPass(err, up.Image) {
			_ = f.storage.Delete(ctx, scope, key)
			return nil, err
		}
		err = nil
	}
	return pass, nil
}
