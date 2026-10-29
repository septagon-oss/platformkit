package admin

import (
	"github.com/septagon-oss/platformkit/kit/screen"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
)

func screenViews(deps Deps) []screen.View {
	signin := admincontracts.Signin{Address: deps.SignIn, OperationID: deps.SignInOperationID}
	if r := deps.Registration; r != nil {
		kind := admincontracts.KindEmail
		if r.Kind == RegistrationKindPassword {
			kind = admincontracts.KindPassword
		}
		signin.Registration = &admincontracts.Registration{Address: r.Address, OperationID: r.OperationID, Kind: kind}
	}
	return admincontracts.Views(signin, deps.Roles != nil, deps.Sessions != nil)
}
