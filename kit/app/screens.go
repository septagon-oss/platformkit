package app

import (
	"bytes"
	"cmp"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/screen"
)

func (a *App) validateScreens(api *httpx.API) error {
	views := []screen.View{}
	for _, m := range a.mods {
		views = append(views, m.Views...)
	}
	inventory, err := api.ScreenInventory(cmp.Or(a.opts.App.String(), "platformkit"), present.PolicyVersion, views)
	if err != nil {
		return fmt.Errorf("app: invalid screens: %w", err)
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		return err
	}
	if a.declaredScreens != nil && !bytes.Equal(a.declaredScreens, encoded) {
		return fmt.Errorf("app: screen declarations changed between route registrations")
	}
	a.declaredScreens, a.screenInventory = encoded, inventory
	return nil
}

// Screens runs the existing dry route gates and returns their validated screen
// inventory. It opens no service and releases its declaration claims on return.
func (a *App) Screens() (screen.Inventory, error) {
	defer a.giveBackDeclared()
	if err := a.Declarations(); err != nil {
		return screen.Inventory{}, err
	}
	return a.screenInventory, nil
}
