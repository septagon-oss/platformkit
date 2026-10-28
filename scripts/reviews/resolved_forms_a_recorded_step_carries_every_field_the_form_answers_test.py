#!/usr/bin/env python3
"""A recorded stepped form carries every field its form answers and names its button.

The kernel resolves a stepped form once and publishes the answer as the record's resolved
layout; every client — the web shell, a generated mobile client — builds its screens from
that answer alone: one screen per member of `steps`, and one primary button per screen
whose copy the answer gives. Nothing downstream can add a field a step left out, because
then the field is on no screen and no save of that form can carry a value for it, and
nothing can repair a button whose copy asks for an answer no field of that step holds.

So the answer carries its own completeness, and this asks it of the recorded answers a
composition gives, reading a stepped form as a strict client does: through the `forms`
member of a resolved layout, whose spelling tells it apart from a field's editability.
The published documents themselves are asked in the sibling checks.

Run: python3 -m pytest -q \
    scripts/reviews/resolved_forms_a_recorded_step_carries_every_field_the_form_answers_test.py
"""
import json
import os

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
BODIES = [
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-deck.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-choices.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-task-en.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-content-en.json"),
]
# The editable list each stepped answer splits into steps, published beside it. The input
# form prints no such list of its own: its membership is the create form's.
EDITABLE = {"createForm": "create", "updateForm": "update", "inputForm": None}


def stepped_forms():
    """Every stepped answer a recorded composition gives, with the record that carries it."""
    found = []
    for path in BODIES:
        with open(path, encoding="utf-8") as handle:
            recorded = json.load(handle)
        entries = [(f"resources[{index}]", entry)
                   for index, entry in enumerate(recorded.get("resources", []) or [])]
        if isinstance(recorded.get("resolved"), dict):
            entries.append(("", recorded))
        for where, entry in entries:
            body = entry.get("resolved") if where else entry
            layout, fields = body.get("layout"), entry.get("fields")
            forms = layout.get("forms") if isinstance(layout, dict) else None
            if not isinstance(forms, dict):
                continue
            names = [field.get("name") for field in fields or [] if isinstance(field, dict)]
            assert names, f"{os.path.basename(path)}:{where} records a form with no field of its own"
            for name, answer in sorted(forms.items()):
                at = f"{os.path.basename(path)}:{where}.layout.forms.{name}".lstrip(".")
                assert name in EDITABLE, f"{at}: no stepped form of a record carries that name"
                if isinstance(answer, dict) and "steps" in answer:
                    found.append((at, name, layout, answer, names))
    assert found, ("no recorded answer a composition gives carries a stepped form, so this case "
                   "would say nothing about the screens a client builds from one")
    return found


def test_a_recorded_step_holds_every_field_its_form_answers_and_no_field_twice():
    for at, name, layout, answer, names in stepped_forms():
        assert answer.get("layout") == "steps", (
            f"{at}: an answer that prints steps is read as stepped, and it answers "
            f"{answer.get('layout')!r}")
        steps = answer["steps"]
        assert steps, f"{at}: a stepped answer with no step is a form nobody can complete"
        held = [field for step in steps for field in step["fields"]]
        assert len(held) == len(set(held)), (
            f"{at}: its steps hold {held}, which names one field twice, so the person answers "
            "it on two screens and the save cannot tell which answer was meant")
        for step in steps:
            assert step["fields"], (
                f"{at}: step {step['key']!r} holds no field, so its screen asks nothing")
            unknown = sorted(set(step["fields"]) - set(names))
            assert not unknown, (
                f"{at}: step {step['key']!r} holds {unknown}, which the record names no such "
                "field, so no screen can draw them and no command accepts them")
        editable = layout.get(EDITABLE[name]) if EDITABLE[name] else None
        if editable is not None:
            assert held == list(editable), (
                f"{at}: its steps hold {held} in that order, while the same layout answers "
                f"{name}'s editable fields as {editable}: a field the steps leave out is on no "
                "screen, and a save of this form could never carry a value for it")


def test_a_recorded_step_names_itself_and_its_primary_button():
    for at, name, layout, answer, names in stepped_forms():
        keys = [step["key"] for step in answer["steps"]]
        assert all(keys) and len(set(keys)) == len(keys), (
            f"{at}: its steps are keyed {keys}, and Back, a reload and an error that returns "
            "to its step all name a step by that key")
        assert all(step["label"].strip() for step in answer["steps"]), (
            f"{at}: every step states the label its screen shows: {keys}")
        for step in answer["steps"]:
            label, asked = step["primaryLabel"], step["primaryLabel"].count("{answer}")
            assert label.strip() and asked <= 1 and "{" not in label.replace("{answer}", ""), (
                f"{at}: step {step['key']!r} answers with {label!r}, which is no button: a step "
                "asks either with its own words or with those words around one answer")
            if asked:
                assert step["answerField"] in step["fields"] and step["emptyLabel"].strip(), (
                    f"{at}: step {step['key']!r} answers with {label!r}, which asks for "
                    f"{step['answerField']!r} — held by no step of this answer — or leaves no "
                    "words for the screen before any answer exists")
            else:
                assert not step["answerField"] and not step["emptyLabel"], (
                    f"{at}: step {step['key']!r} answers with the literal {label!r} while "
                    f"naming {step['answerField']!r} and {step['emptyLabel']!r}, which no "
                    "button prints")
