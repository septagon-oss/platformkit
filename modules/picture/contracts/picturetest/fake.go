// Package picturetest supplies scripted sources and the shared pure decision adapter.
package picturetest

import (
	"context"
	"sync"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

// Fake scripts source responses. It models neither SQL isolation nor authorization.
// Hook observes the exact request transaction before a detached answer is returned.
type Fake struct {
	mu    sync.Mutex
	Input contracts.Input
	Err   error
	Calls int
	Hook  func(context.Context, db.Tx[db.Tenant], string)
}

// Load records a call and copies its scripted value; an error returns zero Input.
// Configure exported fields only while no calls are running.
func (f *Fake) Load(ctx context.Context, tx db.Tx[db.Tenant], key string) (contracts.Input, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Hook != nil {
		f.Hook(ctx, tx, key)
	}
	if f.Err != nil {
		return contracts.Input{}, f.Err
	}
	in := f.Input
	in.Value.Options = append([]contracts.Selection{}, in.Value.Options...)
	in.Value.Colours = append([]string{}, in.Value.Colours...)
	return in, nil
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
