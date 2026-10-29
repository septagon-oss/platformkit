package contracts

import "github.com/septagon-oss/platformkit/kit/screen"

// HomeView and PageView name public content screens, not authored row content.
const (
	HomeView screen.ViewRef = "public/web/custom/home"
	PageView screen.ViewRef = "public/web/custom/page"
)

// Views returns fresh immutable descriptors with no anchors for authored content.
func Views() []screen.View {
	return []screen.View{
		screen.MustView(screen.ViewSpec{Ref: HomeView, Cardinality: screen.One}),
		screen.MustView(screen.ViewSpec{Ref: PageView, Cardinality: screen.One}),
	}
}
