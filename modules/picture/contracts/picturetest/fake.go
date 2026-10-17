// Package picturetest supplies a refusing source and the shared pure decision adapter.
package picturetest

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

// Fake delegates pure decisions and refuses every source read. It models neither
// SQL isolation nor authorization; products prove those with their real Source.
type Fake struct{}

// Load returns zero Input and ErrUnavailable for every request.
func (*Fake) Load(context.Context, db.Tx[db.Tenant], string) (contracts.Input, error) {
	return contracts.Input{}, contracts.ErrUnavailable
}

// Declare delegates to the production declaration decision.
func (f *Fake) Declare(name, svg string) (contracts.Template, error) {
	return contracts.Compile(name, svg)
}

// Render delegates validation and composition to the production pure decision.
func (f *Fake) Render(t contracts.Template, v contracts.Value) (string, error) {
	r, err := contracts.Resolve(t, v)
	if err != nil {
		return "", err
	}
	return contracts.Compose(t, r)
}
