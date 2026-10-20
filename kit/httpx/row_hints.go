package httpx

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// CheckRowHints returns the first invalid row-action declaration, or empty.
// Call after every resource and command is registered, before serving requests.
// Manual command registrants must truthfully declare Fields and EmptyInputFault.
func CheckRowHints(resources []Resource) string {
	for _, r := range resources {
		owner := r.Module + "/" + r.Entity
		if bad := entity.DeckFault(r.Present, r.Schema.Fields); bad != "" {
			return owner + " " + bad
		}
		if name := r.Present.PrimaryCommand; name != "" {
			c, found := namedCommand(r.Commands, name)
			if !found {
				return fmt.Sprintf("%s primaryCommand %q is not a command of %s", owner, name, r.Entity)
			}
			if len(c.Fields) > 0 {
				return fmt.Sprintf("%s primaryCommand %q takes arguments; a row's swipe sends none", owner, name)
			}
			if bad := rowCommandFault(c); bad != "" {
				return fmt.Sprintf("%s primaryCommand %q %s", owner, name, bad)
			}
		}
		if r.Present.Deck == nil {
			continue
		}
		if r.Singleton || r.CommandsOnly || !r.Offers(CRUDList) {
			return owner + " deck requires a list resource"
		}
		for _, key := range []string{"right", "left", "up", "down"} {
			d, ok := r.Present.Deck.Directions[key]
			if !ok || d.Skip {
				continue
			}
			path := owner + " deck.directions." + key + ".command"
			c, found := namedCommand(r.Commands, d.Command)
			if !found {
				return fmt.Sprintf("%s %q is not a command of %s", path, d.Command, r.Entity)
			}
			if bad := rowCommandFault(c); bad != "" {
				return fmt.Sprintf("%s %q %s", path, c.Verb, bad)
			}
			for _, f := range c.Fields {
				if f.Default != "" {
					if _, err := entity.DefaultValue(f); err != nil {
						return fmt.Sprintf("%s %q: %s", path, c.Verb, err)
					}
				}
				if f.Default == "" {
					return fmt.Sprintf("%s %q argument %q has no explicit default", path, c.Verb, f.Name)
				}
			}
		}
	}
	return ""
}

func namedCommand(commands []Command, verb string) (Command, bool) {
	for _, c := range commands {
		if c.Verb == verb {
			return c, true
		}
	}
	return Command{}, false
}

func rowCommandFault(c Command) string {
	switch {
	case c.Present.System:
		return "is a system command; no person is offered it"
	case c.Collection:
		return "runs on the collection; a row names one row"
	case c.EmptyInputFault != "":
		return c.EmptyInputFault
	}
	return ""
}
