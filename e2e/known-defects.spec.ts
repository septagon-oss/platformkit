// Known defects, recorded where the gate can see them.
//
// This file exists so that an unfixed finding cannot become folklore. Each test asserts
// what the shell *should* do, and declares itself expected to fail. Today the run stays
// green because these tests fail. The day somebody fixes one, it starts passing, Playwright
// reports it as an error — "test was expected to fail, but passed" — and the run goes red
// until this entry is deleted. A defect listed here therefore has a half-life: it either
// gets fixed or it gets argued out of existence, and it cannot quietly become normal.
//
// Nothing here belongs in design-audit.spec.ts. That file measures what the family claims
// to have achieved; this one measures what it has not, and the two must never be conflated
// by an `expect.soft` or a TODO comment.
//
// It holds no defect today: both of its entries were fixed on 2026-09-29, and the file stays so the
// next unfixed finding has the place this header describes. A new entry is a `test(...)` that calls
// `test.fail()` and signs in the way e2e/admin-navigation.spec.ts does.

// The section navigation below the sidebar breakpoint was fixed by components.SidebarDisclosure and
// left this file for e2e/admin-navigation.spec.ts, where it is expected to pass.

// The keyboard scroll of a focused region was fixed by the browser itself and left this file for
// e2e/scroll-regions.spec.ts, where it is expected to pass.

export {};
