#!/usr/bin/env bash
# Every layer that renders markup must be able to render it alone.
#   check_ui_layers.sh [repository]
#
# The composition this repository claims is: the foundation emits components and
# derived screens, a capability emits its own namespaced markup when derivation is
# not enough, and the application composes them and writes no markup of its own.
# None of that is checked anywhere. `check_imports.sh` and Go's own `internal/`
# rule keep one module from importing another module's UI — the compiler is a
# better gate than a script — but neither notices these two failures, both of
# which produce markup that composes into an app and quietly does nothing:
#
#   1. A raw utility class (`Class("bg-red-500")`). It paints, so the page looks
#      right in the theme it was written against, and it is outside the token
#      system: a second palette assembled one class at a time, invisible to a
#      theme switch and to `designexport`. A layer names a *thing*
#      (`plan-day-board`) and lets its own stylesheet say what it looks like, or
#      it asks the foundation for a token (`style.SurfaceBrandSoft`).
#
#   2. A class the layer emits and nobody styles. `.home-hero-copy` with no rule
#      for it anywhere is a hook with nothing behind it: the element inherits,
#      the markup reads as if it were designed, and the first person to change
#      the layout discovers the styling was never there.
#
# So: a namespaced class must be answered by a selector in the same layer, and a
# utility-looking class must not be written at all. Both are counted per layer
# (`modules/<name>`, `apps/<name>`) because "self-contained" is a claim about one
# layer, and an answer found in a neighbouring module is the drift in disguise.
#
# Tests are skipped: they quote markup rather than ship it. Single-word classes are
# skipped too — those are most often foundation utilities (`visually-hidden`), and
# a rule that guesses is a rule that gets disabled.
set -euo pipefail
root="$(cd "${1:-$(dirname "${BASH_SOURCE[0]}")/..}" && pwd)"
cd "$root"

# Prefixes that mean "this is a layout or paint instruction, not a name". Drawn
# from the utility vocabulary the foundation compiles, not invented here.
utility='^(p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|bg|text|border|rounded|flex|grid|w|h|max-w|min-w|max-h|min-h|gap|space|items|justify|content|self|order|shadow|font|leading|tracking|z|inset|top|bottom|left|right|overflow|cursor|transition|animate|opacity|whitespace|hidden)-[[:alnum:]!/_-]+(\[[^]]*\])?(:[[:alnum:]-]+)*$'

sources="$(find modules apps -name '*.go' -not -name '*_test.go' -type f 2>/dev/null | sort)"
if [ -z "$sources" ]; then
    echo "ui layers: no markup sources to read"
    exit 0
fi

raw=""
for file in $sources; do
    # Every Class("...") literal, with each space-separated name inside it.
    names="$(awk -v file="$file" '
        {
            line = $0
            while (match(line, /Class\("[^"]*"\)/)) {
                # 7 for `Class("`, 9 more to drop the trailing `")`. Get this wrong
                # by one and the last class in every attribute is tested with a `"`
                # glued to it, so no rule ever matches it and the gate passes while
                # claiming to have read the file. That bug was caught by seeding a
                # violation and watching the gate say nothing.
                lit = substr(line, RSTART + 7, RLENGTH - 9)
                n = split(lit, parts, /[[:space:]]+/)
                for (i = 1; i <= n; i++) if (parts[i] != "") print FILENAME ":" FNR ":" parts[i]
                line = substr(line, RSTART + RLENGTH)
            }
        }' "$file")"
    while IFS= read -r entry; do
        [ -n "$entry" ] || continue
        class="${entry##*:}"
        if printf '%s' "$class" | grep -qE "$utility"; then
            raw="${raw}${entry}
"
        fi
    done <<<"$names"
done
if [ -n "$raw" ]; then
    printf '%s' "$raw" | sed '/^$/d' >&2
    echo "RAW UTILITY CLASSES: markup above asks for a paint instruction instead of a name or a token. Use style.* / css.Decl in the layer's stylesheet, or a foundation class." >&2
    exit 1
fi

unstyled=""
layers="$( { printf '%s\n' $sources | sed -nE 's#^(modules|apps)/([^/]+)/.*#\1/\2#p' || true; } | sort -u)"
for layer in $layers; do
    # `|| true` on anything below that may legitimately find nothing: with
    # pipefail, a layer with no markup would abort the gate silently, and a gate
    # that exits 1 without a sentence is read as a crash, then disabled.
    #
    # One line per class attribute as written, not one per word. `Class("home-contact
    # home-wrap")` is a styled element — home-wrap gives it its measure — and calling
    # the other name a naked hook is a false alarm four times in five. What is not
    # excused is a class written on its own that no rule answers: markup claiming a
    # design nobody wrote.
    literals="$( { grep -rhoE 'Class\("[^"]*"\)' "$layer" --include='*.go' || true; } |
        sed -E 's/Class\("(.*)"\)/\1/' | sort -u)"
    count=0
    while IFS= read -r literal; do
        [ -n "$literal" ] || continue
        named=0
        for class in $literal; do
            printf '%s' "$class" | grep -qE '^[a-z][a-z0-9]*(-[a-z0-9]+)+$' || continue
            named=$((named + 1)); count=$((count + 1))
        done
        [ "$named" -gt 0 ] || continue
        # A selector anywhere in this layer answers the element: `.class` with the
        # name ending there, so `.plan-timeline` is not satisfied by the rule for
        # `.plan-timeline-item`, and a closing quote counts because
        # Select(".plan-timeline-item") is how a Go stylesheet writes one.
        answered=0
        for class in $literal; do
            if grep -rqE "\.${class}([^a-z0-9_-]|$)" "$layer" 2>/dev/null; then answered=1; break; fi
        done
        [ "$answered" -eq 1 ] && continue
        for class in $literal; do
            printf '%s' "$class" | grep -qE '^[a-z][a-z0-9]*(-[a-z0-9]+)+$' || continue
            unstyled="${unstyled}${layer}: .${class}
"
        done
    done <<<"$literals"
    # Layers that write no markup say nothing: a gate that prints a line for every
    # package in the tree turns `make test` into noise, and noise gets skimmed.
    [ "$count" -gt 0 ] && printf 'ui layers: %-34s %3d namespaced classes\n' "$layer" "$count" >&2
done
if [ -n "$unstyled" ]; then
    printf '%s' "$unstyled" | sed '/^$/d' >&2
    echo "UNSTYLED CLASSES: emitted alone, with no selector behind them in their own layer. Style them there, or delete the hook." >&2
    exit 1
fi
echo "ui layers: every namespaced class is styled by the layer that emits it, and no markup reaches past the tokens"
