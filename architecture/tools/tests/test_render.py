"""Tests for the renderer. Layout is derived; views only select model content."""
from __future__ import annotations

import shutil
import sys
from pathlib import Path

import pytest

TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))
import render  # noqa: E402

ARCH = Path(__file__).resolve().parents[2]
FIXTURE = Path(__file__).resolve().parent / "fixtures" / "model"


@pytest.fixture()
def model(tmp_path: Path) -> Path:
    dst = tmp_path / "architecture"
    dst.mkdir()
    shutil.copy2(ARCH / "archmap.schema.json", dst / "archmap.schema.json")
    shutil.copytree(FIXTURE, dst, dirs_exist_ok=True)
    return dst


def test_structure_view_renders_selected_elements_and_relations(model):
    dot = render.render(model, "placeholder-core")
    assert '"placeholder-user" -> "placeholder-api" [label="calls", style=dashed' in dot
    assert '"placeholder-api.placeholder-service"' in dot
    assert 'placeholder-repository' not in dot
    assert "pos=" not in dot


def test_flow_view_renders_ordered_steps(model):
    index = model / "index.yaml"
    index.write_text(
        index.read_text().replace(
            "views:\n",
            'views:\n  placeholder-flow:\n    summary: "TODO: flow projection for the primary scenario."\n',
            1,
        )
    )
    (model / "views/placeholder-flow.yaml").write_text(
        """archmap: 2
doc: view
id: placeholder-flow
verified:
  revision: \"0000000\"
  verified_on: \"1970-01-01\"
kind: flow
flow: placeholder-happy-path
"""
    )
    dot = render.render(model, "placeholder-flow")
    assert "rankdir=TB" in dot
    assert 'label="1. placeholder-user → placeholder-api.placeholder-handler\\nTODO: sends the request"' in dot
    assert 'label="4. placeholder-api.placeholder-repository → placeholder-db\\nTODO: writes the record\\nreturns: TODO: what comes back"' in dot
    assert '"step-1" -> "step-2"' in dot
    assert dot.index('"step-1"') < dot.index('"step-4"')
