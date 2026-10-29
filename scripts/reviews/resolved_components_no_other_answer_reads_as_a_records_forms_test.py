#!/usr/bin/env python3
"""Nothing another component can print reads as a record's stepped forms.

The kernel prints a resolved presentation policy from the types of kit/entity/present and
publishes its shape as an OpenAPI document that clients are generated against strictly.
Every resolved component closes its members with `additionalProperties: false`, and that
closure is what lets a reader name the component an object answers from the members it
holds: an object whose keys cover a component's required members and hold nothing outside
that component's published properties is read as one of them. The conformance checks this
repository runs against the served document identify objects that way, and a strict
generated client reads the members by the same names.

A component that leaves a member optional therefore tells its objects from another's only
through a member that other one cannot print: an object that covers this component's
required members while naming nothing it does not publish answers this component too, and
no reader can correct the reading, because the document carries no other name for that
object. Whichever component is asked about, the document then answers correctly about the
wrong object.

This asks the question of one component family only — the stepped-form answers a record
publishes — and of the two published documents and every resolved body recorded as an
answer a composition gives. The recorded reading is what a client meets first, and the
published reading is asked even where no body has recorded the answer yet, so a shape
nobody has recorded is still refused before it is served.

Run: python3 -m pytest -q \
    scripts/reviews/resolved_components_no_other_answer_reads_as_a_records_forms_test.py
"""
import json
import os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DOCUMENTS = ["openapi.json", "openapi.int64.json"]
FORMS = ["ResolvedForms", "ResolvedForm", "ResolvedStep"]
BODIES = [
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-deck.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-choices.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-task-en.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-content-en.json"),
]


def schemas_of(name):
    path = os.path.join(ROOT, "apps", "platformkit", "testdata", name)
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)["components"]["schemas"]


def closed(schemas):
    """The components that close their members, which are the ones read by member names."""
    return {name: schema for name, schema in schemas.items()
            if schema.get("additionalProperties") is False}


def members(schema, required_only=False):
    if required_only:
        return set(schema.get("required") or [])
    return set(schema.get("properties") or {})


def resolved_bodies():
    """Every resolved policy recorded as an answer a composition gives, with where it sits."""
    out = []
    for path in BODIES:
        with open(path, encoding="utf-8") as handle:
            recorded = json.load(handle)
        for index, resource in enumerate(recorded.get("resources", []) or []):
            if isinstance(resource, dict) and isinstance(resource.get("resolved"), dict):
                out.append((os.path.basename(path), f"resources[{index}].resolved",
                            resource["resolved"]))
        if isinstance(recorded.get("resolved"), dict):
            out.append((os.path.basename(path), "resolved", recorded["resolved"]))
    assert out, "no resolved body was recorded anywhere: this case would say nothing"
    return out


def objects(policy):
    """Every object of a recorded policy, as [(where inside it, the object), ...]."""
    seen, stack = [], [("", policy)]
    while stack:
        where, value = stack.pop()
        if isinstance(value, dict):
            seen.append((where, value))
            for key, member in value.items():
                stack.append((f"{where}.{key}" if where else key, member))
        elif isinstance(value, list):
            for index, item in enumerate(value):
                stack.append((f"{where}[{index}]", item))
    return seen


def reads_as(schema, held):
    """Whether an object holding exactly these members is read as this component."""
    return members(schema, True) <= held <= members(schema)


def explains(schemas, names):
    return "; ".join(f"{name} requires {sorted(members(schemas[name], True))} and publishes "
                     f"{sorted(members(schemas[name]))}" for name in names)


def test_no_other_components_answer_reads_as_a_records_forms():
    """Every other closed component prints a member the stepped answers do not publish.

    A reader names an object by what it holds, so a component that prints nothing outside a
    stepped answer's members, and prints everything that answer requires, is read as that
    answer every time it prints at all: no object it can produce tells the two apart.
    """
    for name in DOCUMENTS:
        schemas = schemas_of(name)
        components = closed(schemas)
        for form in FORMS:
            assert form in components, f"{name} publishes {form} without closing its members"
            required, published = members(components[form], True), members(components[form])
            for other, schema in sorted(components.items()):
                if other == form:
                    continue
                assert not (required <= members(schema, True) and members(schema) <= published), (
                    f"{name}: every {other} answer is read as {form}'s: {explains(schemas, [form, other])}, "
                    f"so a {other} object covers what {form} requires and names no member {form} does not publish")


def test_a_recorded_object_of_the_records_forms_answers_one_component():
    """Where a stepped answer is recorded, no recorded object answers it and something else."""
    for name in DOCUMENTS:
        schemas = schemas_of(name)
        components = closed(schemas)
        for body_file, where, body in resolved_bodies():
            for path, holder in objects(body):
                held = set(holder)
                matched = sorted(component for component, schema in components.items()
                                 if reads_as(schema, held))
                assert not (set(matched) & set(FORMS)) or len(matched) == 1, (
                    f"{name}: the object at {body_file}:{path or '(root)'} holding {sorted(held)} answers "
                    f"both {' and '.join(matched)} — {explains(schemas, matched)}, so the members meant to "
                    "tell one component's object from the other's name both")
