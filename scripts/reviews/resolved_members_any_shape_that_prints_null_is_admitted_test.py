#!/usr/bin/env python3
"""Every member the resolved producer can print as null is admitted by both documents.

`encoding/json` prints null for four Go shapes: a pointer, a slice, a map and an
unassigned interface. The published documents demand most of the members of those shapes,
so the promise a generated reader holds is not a pointer promise — the resolved types
hold nil slices, an unset map and an interface beside their pointers, and any of them can
arrive as null at a client generated from the kernel's own document.

Two things are asked of every such member, of each document the composition checks in
(`apps/platformkit/testdata/openapi.json` and `openapi.int64.json`), judged at the
component its own declaring type publishes:

  * a member that can print null and that the document demands either publishes a schema
    admitting null — a type set holding "null", or a schema that constrains nothing — or
    the producer never leaves it nil, which is a fact read from the Go source that builds
    the type, not from the document;
  * a member that can print null and that the document does not demand is still published,
    so a reader generated from the document has something to read it into.

The second case asks the one demanded member whose schema refuses null,
`ResolvedDeck.directions`, of every resolved body this repository recorded: a recorded
body never holds it as null. A map is printed as null only when it is nil, so what keeps
the pair agreeing is the allocation in kit/entity/present/resolve.go, which no schema
states and no tag reader can see.

The census guards its own emptiness: the case refuses when one of the four shapes
disappears from what it read, so a reader that stopped seeing a shape cannot pass.

Run: python3 -m pytest -q \
    scripts/reviews/resolved_members_any_shape_that_prints_null_is_admitted_test.py
"""
import glob
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PRESENT = os.path.join(ROOT, "kit", "entity", "present")
ENTITY = os.path.join(ROOT, "kit", "entity")
TESTDATA = os.path.join(ROOT, "apps", "platformkit", "testdata")
DOCUMENTS = ["openapi.json", "openapi.int64.json"]

STRUCT = re.compile(r"^type\s+([A-Z]\w*)\s+struct\s*\{")
INTERFACE = re.compile(r"^type\s+([A-Z]\w*)\s+interface\s*\{")
TAGGED = re.compile("^(?:\\s*[A-Za-z_]\\w*\\s+)(\\S+)\\s+`json:\"(\\w+)(?:,[^\"]*)?\"")

# encoding/json prints null for exactly these shapes; it prints anything else as a value.
POINTER, SLICE, MAP, SHAPE_INTERFACE, VALUE = "pointer", "slice", "map", "interface", "value"

# A demanded member whose published schema refuses null is a defect unless the producer
# cannot leave its shape nil. Each guarantee below is checked against the source, so a
# member joins this set by the code that builds it and leaves it when that code changes.
PRODUCER_ALLOCATES = {("ResolvedDeck", "directions"): "ResolvedDeck"}


def package_source():
    """The resolved package's own Go source, tests excluded."""
    return [(os.path.basename(path), open(path, encoding="utf-8").read())
            for path in sorted(glob.glob(os.path.join(PRESENT, "*.go")))
            if not path.endswith("_test.go")]


def interface_names():
    """Every interface type the entity packages declare, named without its package qualifier."""
    found = set()
    for pattern in (os.path.join(ENTITY, "*.go"), os.path.join(PRESENT, "*.go")):
        for path in glob.glob(pattern):
            if path.endswith("_test.go"):
                continue
            with open(path, encoding="utf-8") as handle:
                for line in handle:
                    stated = INTERFACE.match(line)
                    if stated:
                        found.add(stated.group(1))
    return found


def shape(go_type, interfaces):
    stated = go_type.rstrip("`")
    if stated.startswith("*"):
        return POINTER
    if stated.startswith("[]"):
        return SLICE
    if stated.startswith("map["):
        return MAP
    if stated.split(".")[-1] in interfaces:
        return SHAPE_INTERFACE
    return VALUE


def null_printable_members():
    """{(component, member): shape} for every resolved member encoding/json can print as null."""
    interfaces = interface_names()
    out = {}
    for _, source in package_source():
        current = None
        for line in source.splitlines():
            stated = STRUCT.match(line)
            if stated:
                current = stated.group(1)
                continue
            if current and line.rstrip().endswith("}"):
                current = None
                continue
            if not current:
                continue
            declared = TAGGED.match(line)
            if not declared:
                continue
            found = shape(declared.group(1), interfaces)
            if found != VALUE:
                out[(current, declared.group(2))] = found
    return out


def struct_literals(source, name):
    """The body of every `name{...}` composite literal in `source`, braces balanced."""
    bodies = []
    for match in re.finditer(re.escape(name) + r"\{", source):
        depth, index = 1, match.end()
        while index < len(source) and depth:
            if source[index] == "{":
                depth += 1
            elif source[index] == "}":
                depth -= 1
            index += 1
        bodies.append(source[match.end():index - 1])
    return bodies


def allocated(name, member):
    """Whether every construction of `name` in the package allocates `member` as a map."""
    bodies = []
    for _, source in package_source():
        bodies.extend(struct_literals(source, name))
    assert bodies, f"the resolved package builds no {name}: the guarantee says nothing"
    # The Go field name and the JSON member name differ only by their initial letter.
    allocates = re.compile(member + r"\s*:\s*map\[", re.IGNORECASE)
    return all(allocates.search(body) for body in bodies), len(bodies)


def schemas(document):
    return document["components"]["schemas"]


def load():
    return {name: json.load(open(os.path.join(TESTDATA, name), encoding="utf-8"))
            for name in DOCUMENTS}


def type_names(published):
    stated = published.get("type")
    if isinstance(stated, str):
        return [stated]
    if isinstance(stated, list):
        return [str(item) for item in stated]
    return []


def admits_null(published):
    """Whether the published schema takes a null: a type set holding it, or no rule at all."""
    if "null" in type_names(published):
        return True
    return not published or set(published) <= {"$ref", "description", "title"}


def recorded_objects():
    """Every object of every JSON body recorded under a ui/ or apps/ testdata directory."""
    documents = {"openapi.json", "openapi.int64.json"}
    paths = (glob.glob(os.path.join(ROOT, "ui", "**", "testdata", "**", "*.json"), recursive=True)
             + glob.glob(os.path.join(ROOT, "apps", "**", "testdata", "**", "*.json"), recursive=True))
    for path in paths:
        if os.path.basename(path) in documents:
            continue
        try:
            with open(path, encoding="utf-8") as handle:
                data = json.load(handle)
        except (OSError, ValueError):
            continue
        stack = [data]
        while stack:
            value = stack.pop()
            if isinstance(value, dict):
                yield os.path.relpath(path, ROOT), value
                stack.extend(value.values())
            elif isinstance(value, list):
                stack.extend(value)


def test_no_required_resolved_member_prints_a_null_the_published_documents_refuse():
    """Each null-printable member is admitted by both documents at its own component."""
    documents = load()
    members = null_printable_members()
    assert members, "no null-printable member was read from the resolved types: this case would say nothing"
    shapes = set(members.values())
    assert shapes == {POINTER, SLICE, MAP, SHAPE_INTERFACE}, (
        f"the resolved types were read for four null-printable shapes and found {sorted(shapes)}: "
        "a shape went missing from this reader, so the assertions below could not see it")
    refused, guaranteed = [], []
    for (component, member), found in sorted(members.items()):
        for name in DOCUMENTS:
            schema = schemas(documents[name]).get(component)
            assert schema is not None, f"{name} publishes no {component}, which declares {member}"
            published = (schema.get("properties") or {}).get(member)
            assert published is not None, (
                f"{name}: {component}.{member} can be printed as null ({found}) but the document "
                "publishes no such property: a reader generated from it has nothing to read it into")
            if member not in (schema.get("required") or []):
                assert published != {}, (
                    f"{name}: {component}.{member} may be absent and is published as the empty "
                    "schema, so a reader cannot tell an absent member from any other value")
                continue
            if admits_null(published):
                continue
            if (component, member) not in PRODUCER_ALLOCATES:
                refused.append((name, component, member, found, type_names(published)))
                continue
            holds, built = allocated(PRODUCER_ALLOCATES[(component, member)], member)
            assert holds, (
                f"{name}: {component}.{member} is published as {type_names(published)}, never as "
                f"null, and this case is told the producer allocates it — but {built} of the "
                "constructions no longer do, so a nil map prints a null the document refuses")
            guaranteed.append((name, component, member))
    assert not refused, (
        "these members the producer can print as null are demanded by a schema that refuses it, "
        f"so a strict reader refuses the body the kernel serves: {refused}")
    assert len(guaranteed) == len(PRODUCER_ALLOCATES) * len(DOCUMENTS), (
        "a producer guarantee is named for a member no document demands any more: the exception "
        f"would then excuse a defect instead of the allocation that earns it ({guaranteed})")


def test_the_directions_the_deck_document_demands_never_arrives_as_null():
    """The one demanded member whose schema refuses null is a map the resolution always allocates."""
    documents = load()
    for name in DOCUMENTS:
        deck = schemas(documents[name])["ResolvedDeck"]
        assert "directions" in (deck.get("required") or []), (
            f"{name} no longer demands ResolvedDeck.directions: this case guards the null a nil map "
            "prints, and a member that may be absent needs asking about instead")
        assert not admits_null(deck["properties"]["directions"]), (
            f"{name} now admits null for ResolvedDeck.directions, so the null a nil map would print "
            "is admitted and this case reads the wrong member")
    seen = 0
    for path, holder in recorded_objects():
        if isinstance(holder.get("deck"), dict):
            seen += 1
        if "directions" in holder:
            assert holder["directions"] is not None, (
                f"{path} records a resolved body whose directions is null, which the document "
                "publishes as an object and the resolution never prints")
    assert seen, "no recorded resolved body holds a deck: this case would say nothing"


if __name__ == "__main__":
    failures = 0
    for case_name, case in sorted(globals().items()):
        if case_name.startswith("test_") and callable(case):
            try:
                case()
                print(f"ok   {case_name}")
            except AssertionError as problem:
                failures += 1
                print(f"FAIL {case_name}: {problem}")
    sys.exit(1 if failures else 0)
