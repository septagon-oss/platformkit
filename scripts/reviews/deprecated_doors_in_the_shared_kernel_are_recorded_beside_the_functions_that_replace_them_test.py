#!/usr/bin/env python3
"""Every deprecated door in the shared kernel has a removal row, and its row names a real door.

The compatibility rule refuses a removal on a frozen line, and the record in
docs/compatibility/v3.0.0.md is the reading list the next major acts on: a `// Deprecated:`
comment reaches only the reader who opens the file, so the table is the half that reaches the
person deciding what may go. The kernel renamed a set of doors and marked each one, which put
seventeen names into both places at once. Nothing in the tree compares the two lists for these
packages — the one script that compares a package's markers with its rows names kit/rest by
path and answers for no other package.

So this compares them for the four packages the renames touched — kit/events,
kit/events/transport, kit/module, kit/entity — and asks three things no tool otherwise checks:
that a marker without a row is refused, because the next major would remove a name no record
lists; that a row without a marker is refused, because a name earns its way onto the list only
by being deprecated in the code; and that every door a row points a migrating author at is
published by the package it is attributed to, because a row that sends an author to a function
that does not exist is worse than no row at all. The marker's own sentence is asked to name the
same door its row names, so the two halves of one deprecation cannot drift apart line by line,
and the doc comment each row links is asked to declare the name it is the record of.

Run: python3 -m pytest -q scripts/reviews/deprecated_doors_in_the_shared_kernel_are_recorded_beside_the_functions_that_replace_them_test.py
"""
import os
import re

import pytest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
RECORD = os.path.join(ROOT, "docs", "compatibility", "v3.0.0.md")

# The packages whose exported doors the composition's own event catalogue renamed, with the
# name each is imported under. A door is looked up in the directory it is attributed to.
PACKAGES = {
    "kit/events": "events",
    "kit/events/transport": "transport",
    "kit/module": "module",
    "kit/entity": "entity",
}
IMPORTED_BY = {imported: package for package, imported in PACKAGES.items()}

MARKER = re.compile(r"^// Deprecated: (.*)$")
DECLARED = re.compile(r"^(?:func|var|const|type)\s+(?:\([^)]*\)\s*)?([A-Za-z]\w*)\b")
DOOR = re.compile(r"\b([a-z]\w*)\.([A-Z]\w*)\b")
LINK = re.compile(r"\[doc comment\]\(([^)]+)\)")


def source_files(package):
    """The package's own files: a `_test.go` file declares nothing the record may point at."""
    home = os.path.join(ROOT, package)
    return [
        os.path.join(home, entry)
        for entry in sorted(os.listdir(home))
        if entry.endswith(".go") and not entry.endswith("_test.go")
    ]


def declarations(package):
    """Every name one package declares at file scope, exported or not."""
    names = {}
    for path in source_files(package):
        with open(path, encoding="utf-8") as handle:
            for line in handle:
                declared = DECLARED.match(line)
                if declared and declared.group(1)[0].isupper():
                    names.setdefault(declared.group(1), path)
    return names


def markers():
    """Every `// Deprecated:` marker in those packages, as {(package, name): its sentence}.

    A marker belongs to the declaration below it, and the comment lines between them belong to
    the same declaration, because that is how Go reads them.
    """
    found = {}
    for package in sorted(PACKAGES):
        for path in source_files(package):
            with open(path, encoding="utf-8") as handle:
                lines = [line.rstrip("\n") for line in handle]
            for index, line in enumerate(lines):
                marked = MARKER.match(line)
                if not marked:
                    continue
                name = None
                for following in lines[index + 1 :]:
                    if not following.strip():
                        continue
                    if not following.startswith("//"):
                        declared = DECLARED.match(following)
                        if declared:
                            name = declared.group(1)
                        break
                assert name, f"{path}:{index + 1}: a marker sits above no declaration"
                assert (package, name) not in found, f"{path}: {name} is marked twice"
                found[(package, name)] = marked.group(1)
    return found


def record_rows():
    """The record's rows for those packages, as {(package, name): (replacement, link, identifier)}."""
    rows = {}
    with open(RECORD, encoding="utf-8") as handle:
        for line in handle:
            if not line.startswith("| `"):
                continue
            cells = [c.strip() for c in line.strip().strip("|").split("|")]
            identifier = cells[0].strip("`")
            package, _, name = identifier.rpartition(".")
            if package not in PACKAGES or not name:
                continue
            links = LINK.findall(cells[4])
            assert (package, name) not in rows, f"{identifier} is recorded twice"
            rows[(package, name)] = (cells[2], links[0] if links else "", identifier)
    return rows


@pytest.fixture(scope="module")
def rows():
    return record_rows()


def test_the_record_and_the_code_hold_the_same_set_of_deprecated_doors(rows):
    """A marker the record omits is removed unrecorded; a row the code omits was never earned."""
    marked = set(markers())
    listed = set(rows)
    assert marked == listed, (
        f"marked in the code and unrecorded: {sorted(marked - listed)}; "
        f"recorded and unmarked: {sorted(listed - marked)}"
    )
    assert len(marked) >= 17, (
        f"these packages hold {len(marked)} deprecated doors between the two halves; the set "
        "shrinks by a pair when a row is deleted with its marker, so a count that falls by more "
        "than one pair means one side stopped being read"
    )


def test_every_row_points_at_a_door_the_package_it_names_publishes(rows):
    """An author who follows a row must land on a function the record's own package publishes."""
    for (package, name), (replacement, _, identifier) in sorted(rows.items()):
        spans = re.findall(r"`([^`]+)`", replacement)
        assert spans, f"{identifier}: the replacement column names no call to migrate to"
        reached = {
            door
            for span in spans
            for imported, door in DOOR.findall(span)
            if IMPORTED_BY.get(imported) == package
        }
        assert reached, f"{identifier}: the replacement names no door in {package}"
        published = declarations(package)
        for door in sorted(reached):
            assert door in published, (
                f"{identifier}: the record sends a migrating author to {package}.{door}, "
                f"which that package does not declare in any of its files"
            )


def test_the_marker_and_its_row_name_the_same_door(rows):
    """The comment a reader sees and the table a removal acts on cannot disagree."""
    for (package, name), sentence in sorted(markers().items()):
        replacement, _, identifier = rows[(package, name)]
        # The sentence may name the door qualified (`events.NewCatalog`) or bare (`DeclareFor`);
        # a bare name is every capitalised word no dot stands in front of.
        doors = {door for _, door in DOOR.findall(sentence)}
        doors |= set(re.findall(r"(?<![\w.])([A-Z]\w*)\b", sentence))
        assert any(door in replacement for door in doors), (
            f"{identifier}: the marker sends the author to one of {sorted(doors)} and its row "
            f"to {replacement!r}"
        )


def test_every_row_links_a_doc_comment_that_declares_the_name_it_records(rows):
    """A row whose source link is dead, or whose target declares no such name, leaves the removal decision with nothing to read."""
    for (package, name), (_, link, identifier) in sorted(rows.items()):
        assert link, f"{identifier}: the row names no doc comment"
        target = os.path.normpath(os.path.join(ROOT, "docs", "compatibility", link))
        assert os.path.exists(target), f"{identifier}: links {link}, which is not in the tree"
        assert name in declarations(package), (
            f"{identifier}: {package} declares no {name} for the record to point at"
        )
        with open(target, encoding="utf-8") as handle:
            assert f"// Deprecated:" in handle.read(), (
                f"{identifier}: links {link}, which carries no deprecation marker at all"
            )


if __name__ == "__main__":
    raise SystemExit(pytest.main([__file__, "-q"]))
