package pkit

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/screen"
)

// Screens exports the actual composition's dry route contract without opening
// infrastructure. It uses the same plan and registration gates as Build.
func (a *App) Screens(ctx context.Context, d Deployment) (screen.Inventory, error) {
	p, err := a.Plan(d)
	if err != nil {
		return screen.Inventory{}, err
	}
	engine, err := a.startEngine(ctx, p, d, app.All)
	if err != nil {
		return screen.Inventory{}, err
	}
	return engine.Screens()
}
