// Package picture declares authenticated delivery of server-composed inert pictures.
package picture

import (
	"errors"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/internal"
)

func manifest(source contracts.Source, templates contracts.Templates) (module.Module, error) {
	if source == nil {
		return module.Module{}, errors.New("picture: Source is required")
	}
	declared := map[[32]byte]contracts.Template{}
	for _, t := range templates {
		digest := t.Digest()
		if digest == ([32]byte{}) {
			return module.Module{}, errors.New("picture: zero template declaration")
		}
		declared[digest] = t
		if len(declared) > 64 {
			return module.Module{}, errors.New("picture: maximum 64 distinct templates")
		}
	}
	return module.Module{Name: "picture", Routes: func(s httpx.Surfaces) { internal.Register(s.App, source, declared) }}, nil
}
