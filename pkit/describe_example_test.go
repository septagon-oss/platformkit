package pkit_test

// A consumer outside the kernel, reading what an application is composed of
// through nothing but the import path: one app sentence, one deployment, one
// call. This is the shape a CLI, an agent or a deployment pipeline uses, and it
// is executable — Go runs the example and refuses the file when the output
// drifts, so the example cannot go stale in the way a fenced code block can.
//
// What it prints is the resolved composition, in the build order, in a format
// named by its `schema` line. It is not a manifest, a route table or a health
// report: none of those are the resolver's to answer, and none appear here.

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

func ExampleApp_Describe() {
	app := pkit.NewApp("collect").Use(user.Module, wishlist.Module, cart.Module).
		Roles(pkit.Role{Name: "member", Grants: []string{"cart.read"}})

	doc, err := app.Describe(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		fmt.Println("the composition does not resolve:", err)
		return
	}
	// A caller that commits what it printed gets diffable bytes: the document
	// carries no timestamp, so the file only changes when the composition does.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)

	// Output:
	// {
	//   "schema": "platformkit.composition.v1",
	//   "app": "collect",
	//   "environment": "development",
	//   "resolved": true,
	//   "modules": [
	//     {
	//       "name": "user",
	//       "provides": [
	//         "usercontracts.Service"
	//       ]
	//     },
	//     {
	//       "name": "wishlist",
	//       "needs": [
	//         {
	//           "contract": "usercontracts.Service",
	//           "from": "user"
	//         }
	//       ],
	//       "uses": [
	//         {
	//           "contract": "paymentcontracts.Provider",
	//           "from": ""
	//         }
	//       ],
	//       "contributes": [
	//         {
	//           "contract": "cartcontracts.Extension",
	//           "to": [
	//             "cart"
	//           ]
	//         }
	//       ],
	//       "after": [
	//         "user"
	//       ]
	//     },
	//     {
	//       "name": "cart",
	//       "provides": [
	//         "cartcontracts.Service"
	//       ],
	//       "takes": [
	//         {
	//           "contract": "cartcontracts.Extension",
	//           "from": [
	//             "wishlist"
	//           ]
	//         }
	//       ],
	//       "after": [
	//         "wishlist"
	//       ]
	//     }
	//   ],
	//   "roles": [
	//     {
	//       "name": "member",
	//       "grants": [
	//         "cart.read"
	//       ]
	//     }
	//   ],
	//   "problems": []
	// }
}

// ExampleApp_Describe_refused is the other half, and the half a caller has to
// write: a composition that does not resolve answers with the refusal and no
// graph, so the consumer that ignores the error still has nothing to misread.
func ExampleApp_Describe_refused() {
	app := pkit.NewApp("collect").Use(wishlist.Module)

	doc, err := app.Describe(pkit.Deployment{Environment: pkit.Development})
	fmt.Println("refused:", err != nil, "| resolved:", doc.Resolved, "| modules:", len(doc.Modules))
	for _, p := range doc.Problems {
		fmt.Printf("%s: %s\n", p.Cause, p.Sentence)
	}

	// Output:
	// refused: true | resolved: false | modules: 0
	// Use: wishlist needs usercontracts.Service: add user.Module to collect
}
