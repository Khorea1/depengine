#!/usr/bin/env python3
"""Render one archmap view as Graphviz DOT or SVG.

The model chooses *what* a view contains; this renderer chooses *how* to lay it
out. No coordinates or presentation details are written back to the YAML.
"""
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

import validate

HERE = Path(__file__).resolve().parent
DEFAULT_ROOT = HERE.parent


def _quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n") + '"'


def _load(root: Path) -> dict[str, dict]:
    rep = validate.Report()
    docs = validate.load_and_validate_schema(root, rep)
    if not rep.errors:
        validate.check_semantics(root, docs, rep)
    if rep.errors:
        for finding in rep.errors:
            print(finding, file=sys.stderr)
        raise ValueError("archmap has validation errors")
    return docs


def _catalogue_summary(index: dict, kind: str, item_id: str) -> str:
    return index.get(kind, {}).get(item_id, {}).get("summary", item_id)


def _elements(docs: dict[str, dict]) -> dict[str, tuple[str, str]]:
    idx = docs["index.yaml"]
    out: dict[str, tuple[str, str]] = {}
    for eid, external in idx.get("externals", {}).items():
        out[eid] = ("external", external["summary"])
    for uid in idx.get("units", {}):
        out[uid] = ("unit", _catalogue_summary(idx, "units", uid))
    for _, d in docs.items():
        if d["doc"] != "unit":
            continue
        for cid, component in d.get("components", {}).items():
            out[f"{d['id']}.{cid}"] = ("component", component["summary"])
    return out


def _relations(docs: dict[str, dict]) -> list[tuple[str, str, dict]]:
    out: list[tuple[str, str, dict]] = []
    for file, _, relation, owner in validate._iter_relations(docs):
        src = relation["from"]
        dst = relation["to"]
        if owner:
            if src.startswith("."):
                src = owner + src
            if dst.startswith("."):
                dst = owner + dst
        out.append((src, dst, relation))
    return out


def _node_attrs(kind: str, label: str, summary: str) -> str:
    shape = {"external": "oval", "unit": "box", "component": "box"}[kind]
    text = label if kind != "component" else label.split(".", 1)[1]
    tooltip = summary
    return f"label={_quote(text)}, shape={shape}, tooltip={_quote(tooltip)}"


def render_structure(docs: dict[str, dict], view: dict) -> str:
    selected = list(view["elements"])
    selected_set = set(selected)
    elements = _elements(docs)
    kinds = set(view.get("relation_kinds", []))

    lines = ["digraph archmap {", "  rankdir=LR;", '  graph [fontname="sans-serif", nodesep=0.45, ranksep=0.7];', '  node [fontname="sans-serif"];', '  edge [fontname="sans-serif", fontsize=10];']

    # Group selected components under their owning unit without encoding layout in YAML.
    grouped: dict[str, list[str]] = {}
    plain: list[str] = []
    for ref in selected:
        if "." in ref:
            grouped.setdefault(ref.split(".", 1)[0], []).append(ref)
        else:
            plain.append(ref)

    for ref in plain:
        kind, summary = elements[ref]
        lines.append(f"  {_quote(ref)} [{_node_attrs(kind, ref, summary)}];")
    for unit, refs in grouped.items():
        lines.append(f"  subgraph {_quote('cluster-' + unit)} {{")
        lines.append(f"    label={_quote(unit)};")
        for ref in refs:
            kind, summary = elements[ref]
            lines.append(f"    {_quote(ref)} [{_node_attrs(kind, ref, summary)}];")
        lines.append("  }")

    for src, dst, relation in _relations(docs):
        if src not in selected_set or dst not in selected_set:
            continue
        if kinds and relation["kind"] not in kinds:
            continue
        attrs = [f"label={_quote(relation['kind'])}"]
        if relation["confidence"] == "inferred":
            attrs.append("style=dashed")
        if relation.get("via"):
            attrs.append(f"tooltip={_quote(relation['via'])}")
        lines.append(f"  {_quote(src)} -> {_quote(dst)} [{', '.join(attrs)}];")

    lines.append("}")
    return "\n".join(lines) + "\n"


def render_flow(docs: dict[str, dict], view: dict) -> str:
    flow_id = view["flow"]
    flow = docs[f"flows/{flow_id}.yaml"]
    elements = _elements(docs)
    refs: list[str] = []
    for step in flow["steps"]:
        for ref in (step["from"], step["to"]):
            if ref not in refs:
                refs.append(ref)

    lines = ["digraph archmap {", "  rankdir=LR;", '  graph [fontname="sans-serif", nodesep=0.45, ranksep=0.7];', '  node [fontname="sans-serif"];', '  edge [fontname="sans-serif", fontsize=10];']
    for ref in refs:
        kind, summary = elements[ref]
        lines.append(f"  {_quote(ref)} [{_node_attrs(kind, ref, summary)}];")
    for index, step in enumerate(flow["steps"], start=1):
        label = f"{index}. {step['action']}"
        attrs = [f"label={_quote(label)}"]
        if step["confidence"] == "inferred":
            attrs.append("style=dashed")
        lines.append(f"  {_quote(step['from'])} -> {_quote(step['to'])} [{', '.join(a.strip() for a in attrs)}];")
    lines.append("}")
    return "\n".join(lines) + "\n"


def render(root: Path, view_id: str) -> str:
    docs = _load(root)
    rel = f"views/{view_id}.yaml"
    if rel not in docs:
        raise KeyError(f"unknown view {view_id!r}")
    view = docs[rel]
    return render_flow(docs, view) if view["kind"] == "flow" else render_structure(docs, view)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("view", help="view id from views/<id>.yaml")
    ap.add_argument("--root", type=Path, default=DEFAULT_ROOT, help="directory containing index.yaml")
    ap.add_argument("--format", choices=("dot", "svg"), default="dot")
    ap.add_argument("-o", "--output", type=Path, help="write output to this file instead of stdout")
    args = ap.parse_args(argv)

    try:
        dot = render(args.root, args.view)
    except (ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    data = dot
    if args.format == "svg":
        try:
            proc = subprocess.run(["dot", "-Tsvg"], input=dot, capture_output=True, text=True, check=False)
        except FileNotFoundError:
            print("error: Graphviz 'dot' is required for --format svg", file=sys.stderr)
            return 2
        if proc.returncode != 0:
            print(proc.stderr.strip() or "error: Graphviz failed", file=sys.stderr)
            return 2
        data = proc.stdout

    if args.output:
        args.output.write_text(data, encoding="utf-8")
    else:
        sys.stdout.write(data)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
