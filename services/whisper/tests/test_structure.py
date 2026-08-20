"""Shapes in this package that are legal Python and never what was meant."""
import ast
import pathlib

import pytest

PACKAGE = pathlib.Path(__file__).resolve().parent.parent
SOURCES = sorted(
    p for p in PACKAGE.rglob("*.py")
    if "__pycache__" not in p.parts and p.parent.name != "tests"
)


@pytest.mark.parametrize("source", SOURCES, ids=lambda p: p.name)
def test_no_loop_carries_an_else(source):
    """`for … else` is what a conditional removed above a loop leaves behind.

    It parses, it imports, and its body runs on every pass that does not break,
    so the branch that used to be the alternative becomes the rule. Nothing
    short of reading the file catches it, which is why this reads the file.
    """
    tree = ast.parse(source.read_text())
    carried = [
        node.lineno
        for node in ast.walk(tree)
        if isinstance(node, (ast.For, ast.AsyncFor, ast.While)) and node.orelse
    ]

    assert not carried, f"{source.name} has a loop with an else at line(s) {carried}"
