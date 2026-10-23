"""Every scripts/*_test.py that imports a sibling script module finds that module."""
import ast
import pathlib
import sys

here = pathlib.Path(__file__).resolve().parent
missing = []
for path in sorted(here.glob("*_test.py")):
    for node in ast.walk(ast.parse(path.read_text())):
        names = [a.name for a in node.names] if isinstance(node, ast.Import) else []
        if isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            names = [node.module]
        for name in names:
            top = name.split(".")[0]
            if top.endswith("_test") and not (here / f"{top}.py").exists():
                missing.append(f"{path.name} imports {top}")
if missing:
    sys.exit("\n".join(missing))
print("ok")
