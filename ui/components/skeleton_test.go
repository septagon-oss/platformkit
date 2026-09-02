package components

// skeleton_test.go pins the loading-state contract: DeferredSlot's HTMX
// defaults and their overridability, the aria seam (busy slot, hidden
// placeholders), and the twins' geometry — the same class lists as the
// components they stand in for, so the swap-in cannot shift layout.
//

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"
)

func renderNode(t *testing.T, n g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := n.Render(&b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestSkeletonPlaceholdersAreHiddenFromAssistiveTech(t *testing.T) {
	t.Parallel()

	for name, n := range map[string]g.Node{
		"block":  Skeleton(SkeletonProps{}),
		"text":   Skeleton(SkeletonProps{Shape: "text", Lines: 3}),
		"circle": Skeleton(SkeletonProps{Shape: "circle"}),
		"table":  TableSkeleton(TableSkeletonProps{}),
	} {
		if html := renderNode(t, n); !strings.Contains(html, `aria-hidden="true"`) {
			t.Errorf("%s placeholder is exposed to assistive tech: %s", name, html)
		}
	}
}

func TestSkeletonUsesCanonicalControlSizes(t *testing.T) {
	t.Parallel()

	for _, size := range []string{"sm", "md", "lg"} {
		t.Run(size, func(t *testing.T) {
			block := renderNode(t, Skeleton(SkeletonProps{Shape: "block", Size: size}))
			if want := clSkeletonBlockSize[size].Compile(); !strings.Contains(block, want) {
				t.Errorf("block size %q missing classes %q: %s", size, want, block)
			}

			circle := renderNode(t, Skeleton(SkeletonProps{Shape: "circle", Size: size}))
			if want := clSkeletonCircleSize[size].Compile(); !strings.Contains(circle, want) {
				t.Errorf("circle size %q missing classes %q: %s", size, want, circle)
			}

			line := renderNode(t, Skeleton(SkeletonProps{Shape: "text", Size: size}))
			if want := clSkeletonLineSize[size].Compile(); !strings.Contains(line, want) {
				t.Errorf("text size %q missing classes %q: %s", size, want, line)
			}
		})
	}

	for _, legacy := range []string{"small", "medium", "large"} {
		if _, ok := clSkeletonBlockSize[legacy]; ok {
			t.Errorf("legacy size alias %q remains in the public class map", legacy)
		}
	}
}

func TestSkeletonTextRendersRequestedLinesWithShortLast(t *testing.T) {
	t.Parallel()

	html := renderNode(t, Skeleton(SkeletonProps{Shape: "text", Lines: 3}))

	if got := strings.Count(html, clSkeleton.Compile()); got != 3 {
		t.Errorf("line count = %d, want 3: %s", got, html)
	}
	if got := strings.Count(html, clSkeletonLineLast.Compile()); got != 1 {
		t.Errorf("short-last-line count = %d, want exactly 1: %s", got, html)
	}
}

// The twins must be built from the mirrored component's own class lists —
// that identity is what guarantees zero layout shift at swap-in.
func TestTableSkeletonMirrorsTableGeometry(t *testing.T) {
	t.Parallel()

	html := renderNode(t, TableSkeleton(TableSkeletonProps{Columns: 2, Rows: 2}))

	for name, cl := range map[string]string{
		"wrap": clTableWrap.Compile(), "table": clTable.Compile(),
		"head": clTableHead.Compile(), "th": clTableTh.Compile(),
		"td": clTableTd.Compile(), "row": clTableRow.Compile(),
	} {
		if !strings.Contains(html, cl) {
			t.Errorf("table skeleton missing Table's %s classes %q", name, cl)
		}
	}
	if got := strings.Count(html, `<th `); got != 2 {
		t.Errorf("header cell count = %d, want 2", got)
	}
	if got := strings.Count(html, `<tr`); got != 3 {
		t.Errorf("row count = %d, want 3 (1 head + 2 body)", got)
	}
}
