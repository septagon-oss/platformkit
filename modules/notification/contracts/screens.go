package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// InboxView names the inbox page. Its repeated notification rows are not anchors.
const InboxView screen.ViewRef = "app/notification/custom/inbox"

// Views returns the immutable inbox descriptor only when its page is composed.
func Views(enabled bool) []screen.View {
	if !enabled {
		return nil
	}
	return []screen.View{screen.MustView(screen.ViewSpec{Ref: InboxView, Cardinality: screen.One})}
}
