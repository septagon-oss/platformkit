package examples

// DescribeWithLayout opts into source declarations without changing Describe's
// v1 serialization. Unknown ancestors/children still require explicit refusal.
// The declarations themselves are components.LayoutDescription: a layout
// component owns what it says about its root, and this package only reads it.
func (e Example) DescribeWithLayout() (ExampleDescription, error) {
	return e.describe(true)
}
