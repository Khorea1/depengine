#!/usr/bin/env python3
"""Validate an archmap model.

Checks, in order:
  1. YAML 1.2 parse, duplicate keys rejected.
  2. JSON Schema (archmap.schema.json), selected by file location.
  3. Semantics: id/file consistency, reference resolution, relation ownership,
     duplicates, index/file summary sync, notes links.
  4. With --repo: paths and evidence exist in the code repo, revision is known
     and how far behind HEAD it is.
  5. Placeholders (TODO markers etc.): warnings, errors under --strict.

Exit code: 0 ok, 1 errors (or warnings under --strict), 2 usage/setup problem.
"""
from __future__ import annotations

import argparse
import difflib
import json
import re
import subprocess
import sys
from dataclasses import dataclass, field
from datetime import date, datetime
from pathlib import Path
from typing import Any, Iterator

from jsonschema import Draft202012Validator
from ruamel.yaml import YAML
from ruamel.yaml.error import YAMLError

HERE = Path(__file__).resolve().parent
DEFAULT_ROOT = HERE.parent
SCHEMA_FILE = "archmap.schema.json"

PLACEHOLDER_RES = [
    re.compile(r"TODO"),
    re.compile(r"(^|\.)placeholder-"),
    re.compile(r"^0{7,40}$"),
    re.compile(r"^1970-01-01$"),
]


@dataclass
class Finding:
    level: str  # "error" | "warning"
    file: str
    where: str
    message: str

    def __str__(self) -> str:
        loc = f"{self.file}:{self.where}" if self.where else self.file
        return f"{self.level.upper():7} {loc}: {self.message}"


@dataclass
class Report:
    findings: list[Finding] = field(default_factory=list)

    def error(self, file: str, where: str, msg: str) -> None:
        self.findings.append(Finding("error", file, where, msg))

    def warn(self, file: str, where: str, msg: str) -> None:
        self.findings.append(Finding("warning", file, where, msg))

    @property
    def errors(self) -> list[Finding]:
        return [f for f in self.findings if f.level == "error"]

    @property
    def warnings(self) -> list[Finding]:
        return [f for f in self.findings if f.level == "warning"]


# --------------------------------------------------------------------------- #
# loading + schema
# --------------------------------------------------------------------------- #


def _yaml() -> YAML:
    y = YAML(typ="safe", pure=True)
    y.version = (1, 2)  # 'no' / 'on' stay strings
    y.allow_duplicate_keys = False
    return y


def _kind_for(rel: str) -> str:
    if rel == "index.yaml":
        return "index"
    return "container" if rel.startswith("containers/") else "flow"


def _fmt_path(parts: Any) -> str:
    out = ""
    for p in parts:
        out += f"[{p}]" if isinstance(p, int) else (("." if out else "") + str(p))
    return out


def _discover(root: Path, rep: Report) -> list[str]:
    rels = []
    if (root / "index.yaml").is_file():
        rels.append("index.yaml")
    else:
        rep.error("index.yaml", "", "missing: every model needs an index.yaml")
    for sub in ("containers", "flows"):
        d = root / sub
        if not d.is_dir():
            continue
        for p in sorted(d.iterdir()):
            if p.suffix == ".yml":
                rep.error(f"{sub}/{p.name}", "", "use the .yaml extension")
            elif p.suffix == ".yaml":
                rels.append(f"{sub}/{p.name}")
    return rels


def load_and_validate_schema(root: Path, rep: Report) -> dict[str, dict]:
    schema = json.loads((root / SCHEMA_FILE).read_text(encoding="utf-8"))
    yaml = _yaml()
    docs: dict[str, dict] = {}
    for rel in _discover(root, rep):
        try:
            data = yaml.load((root / rel).read_text(encoding="utf-8"))
        except YAMLError as exc:
            rep.error(rel, "", f"YAML error: {str(exc).splitlines()[0]}")
            continue
        if not isinstance(data, dict):
            rep.error(rel, "", "top level must be a mapping")
            continue
        kind = _kind_for(rel)
        if data.get("doc") != kind:
            rep.error(rel, "doc", f"expected doc: {kind} for this location, got {data.get('doc')!r}")
            continue
        wrapper = {"$schema": schema["$schema"], "$defs": schema["$defs"], "$ref": f"#/$defs/{kind}"}
        errs = sorted(Draft202012Validator(wrapper).iter_errors(data), key=lambda e: list(map(str, e.path)))
        for e in errs:
            msg = e.message if len(e.message) <= 240 else e.message[:237] + "..."
            if isinstance(e.instance, (date, datetime)):
                msg += " (quote dates and version-like values)"
            rep.error(rel, _fmt_path(e.absolute_path), msg)
        if not errs:
            docs[rel] = data
    return docs


# --------------------------------------------------------------------------- #
# semantics
# --------------------------------------------------------------------------- #


def _iter_relations(docs: dict[str, dict]) -> Iterator[tuple[str, str, dict, str | None]]:
    """Yield (file, where, relation, owner_container_or_None)."""
    for rel, d in docs.items():
        owner = d["id"] if d["doc"] == "container" else None
        if d["doc"] in ("index", "container"):
            for i, r in enumerate(d.get("relations", [])):
                yield rel, f"relations[{i}]", r, owner


def _iter_steps(docs: dict[str, dict]) -> Iterator[tuple[str, str, dict]]:
    for rel, d in docs.items():
        if d["doc"] == "flow":
            for i, s in enumerate(d["steps"]):
                yield rel, f"steps[{i}]", s


def check_semantics(root: Path, docs: dict[str, dict], rep: Report) -> dict[str, str]:
    """Returns the element table {ref: kind}; empty if the index is unusable."""
    idx = docs.get("index.yaml")
    if idx is None:
        return {}
    externals: dict = idx.get("externals", {})
    containers: dict = idx["containers"]
    flows: dict = idx.get("flows", {})

    for clash in sorted(set(externals) & set(containers)):
        rep.error("index.yaml", f"externals.{clash}", "id is used by both an external and a container")

    # index <-> files
    for kind_dir, doc_kind, catalogue in (("containers", "container", containers), ("flows", "flow", flows)):
        for cid in catalogue:
            rel = f"{kind_dir}/{cid}.yaml"
            if rel not in docs:
                if not (root / rel).is_file():
                    rep.error("index.yaml", f"{kind_dir}.{cid}", f"listed but {rel} does not exist")
                continue
            d = docs[rel]
            if d["id"] != cid:
                rep.error(rel, "id", f"id {d['id']!r} must equal the file name and index key {cid!r}")
            if d["summary"] != catalogue[cid]["summary"]:
                rep.error(rel, "summary", "differs from the summary in index.yaml; keep them identical")
        for rel, d in docs.items():
            if d["doc"] == doc_kind and Path(rel).stem not in catalogue:
                rep.error(rel, "", f"file is not listed under '{kind_dir}' in index.yaml")

    # element table
    elements: dict[str, str] = {e: "external" for e in externals}
    elements.update({c: "container" for c in containers})
    for rel, d in docs.items():
        if d["doc"] == "container":
            for comp in d.get("components", {}):
                elements[f"{d['id']}.{comp}"] = "component"

    def resolve(ref: str, owner: str | None, file: str, where: str) -> str | None:
        full = ref
        if ref.startswith("."):
            if owner is None:
                rep.error(file, where, f"local reference {ref!r} is only allowed inside a container file")
                return None
            full = owner + ref
        if full not in elements:
            hint = difflib.get_close_matches(full, list(elements), n=3, cutoff=0.6)
            rep.error(file, where, f"unknown element {ref!r}" + (f" (did you mean: {', '.join(hint)}?)" if hint else ""))
            return None
        return full

    top = lambda ref: ref.split(".")[0]  # noqa: E731

    # relations
    seen: set[tuple[str, str, str]] = set()
    for file, where, r, owner in _iter_relations(docs):
        src = resolve(r["from"], owner, file, f"{where}.from")
        dst = resolve(r["to"], owner, file, f"{where}.to")
        if src is None or dst is None:
            continue
        if src == dst:
            rep.error(file, where, "from and to are the same element")
        if owner is None:
            if elements[src] != "external":
                rep.error(file, f"{where}.from", f"{src!r} is not an external; put this relation in containers/{src.split('.')[0]}.yaml")
        elif src != owner and not src.startswith(owner + "."):
            home = "index.yaml" if elements[src] == "external" else f"containers/{top(src)}.yaml"
            rep.error(file, f"{where}.from", f"relation must live in the file that owns its source; move it to {home}")
        key = (src, dst, r["kind"])
        if key in seen:
            rep.error(file, where, f"duplicate relation {src} -{r['kind']}-> {dst}")
        seen.add(key)

    # flows
    pairs = {(s, d) for s, d, _ in seen}
    top_pairs = {(top(s), top(d)) for s, d in pairs}
    for file, where, s in _iter_steps(docs):
        src = resolve(s["from"], None, file, f"{where}.from")
        dst = resolve(s["to"], None, file, f"{where}.to")
        if src and dst and (src, dst) not in pairs and (top(src), top(dst)) not in top_pairs:
            rep.warn(file, where, f"no declared relation between {top(src)} and {top(dst)}; add it to the model or mark the step inferred")

    # notes links + isolated elements
    for file, d in docs.items():
        n = d.get("notes")
        if n and not (root / n).is_file():
            rep.error(file, "notes", f"{n} does not exist")
    for name, kind in elements.items():
        if kind != "component" and not any(name == a or name == b or a.startswith(name + ".") or b.startswith(name + ".") for a, b in pairs):
            rep.warn("index.yaml", name, f"{kind} {name!r} has no relations")
    return elements


# --------------------------------------------------------------------------- #
# repo checks
# --------------------------------------------------------------------------- #


def _git(repo: Path, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True)


def check_repo(repo: Path, docs: dict[str, dict], rep: Report) -> None:
    idx = docs.get("index.yaml")
    if idx is not None:
        rev = idx["project"]["source"]["revision"]
        if not PLACEHOLDER_RES[2].match(rev):
            if _git(repo, "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}").returncode != 0:
                rep.error("index.yaml", "project.source.revision", f"commit {rev} not found in {repo}")
            else:
                n = _git(repo, "rev-list", "--count", f"{rev}..HEAD").stdout.strip()
                if n.isdigit() and int(n) > 0:
                    rep.warn("index.yaml", "project.source.revision", f"model is {n} commit(s) behind HEAD; re-verify and bump the revision")

    def exists(file: str, where: str, p: str) -> None:
        if not (repo / p).exists():
            rep.error(file, where, f"path {p!r} does not exist in the repo")

    lines: dict[str, int] = {}

    def check_evidence(file: str, where: str, ev: str) -> None:
        path, _, span = ev.partition(":")
        target = repo / path
        if not target.is_file():
            rep.error(file, where, f"evidence file {path!r} does not exist in the repo")
            return
        if span:
            if path not in lines:
                lines[path] = len(target.read_text(encoding="utf-8", errors="replace").splitlines())
            end = int(span.split("-")[-1])
            if end > lines[path]:
                rep.error(file, where, f"evidence {ev!r} is past end of file ({lines[path]} lines)")

    for file, d in docs.items():
        if d["doc"] == "container":
            exists(file, "path", d["path"])
            for i, p in enumerate(d.get("entrypoints", [])):
                exists(file, f"entrypoints[{i}]", p)
            base = d["path"].rstrip("/")
            for cid, c in d.get("components", {}).items():
                exists(file, f"components.{cid}.path", c["path"])
                if base != "." and not (c["path"] == base or c["path"].startswith(base + "/")):
                    rep.warn(file, f"components.{cid}.path", f"{c['path']!r} is outside the container path {base!r}")
                for i, p in enumerate(c.get("entrypoints", [])):
                    exists(file, f"components.{cid}.entrypoints[{i}]", p)
    for file, where, r, _ in _iter_relations(docs):
        for i, ev in enumerate(r.get("evidence", [])):
            check_evidence(file, f"{where}.evidence[{i}]", ev)
    for file, where, s in _iter_steps(docs):
        for i, ev in enumerate(s.get("evidence", [])):
            check_evidence(file, f"{where}.evidence[{i}]", ev)


# --------------------------------------------------------------------------- #
# placeholders
# --------------------------------------------------------------------------- #


def _walk(node: Any, path: str = "") -> Iterator[tuple[str, str]]:
    if isinstance(node, dict):
        for k, v in node.items():
            sub = f"{path}.{k}" if path else str(k)
            yield sub, str(k)
            yield from _walk(v, sub)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            yield from _walk(v, f"{path}[{i}]")
    elif isinstance(node, str):
        yield path, node


def check_placeholders(docs: dict[str, dict], rep: Report) -> None:
    """One warning per file, so a fresh template does not bury real findings."""
    for file, d in docs.items():
        hits = [where for where, s in _walk(d) if any(p.search(s) for p in PLACEHOLDER_RES)]
        if hits:
            shown = ", ".join(hits[:3]) + (", ..." if len(hits) > 3 else "")
            rep.warn(file, "", f"{len(hits)} placeholder value(s) left, e.g. {shown}")


# --------------------------------------------------------------------------- #
# entry points
# --------------------------------------------------------------------------- #


def run(root: Path, repo: Path | None = None) -> Report:
    rep = Report()
    docs = load_and_validate_schema(root, rep)
    if not rep.errors:  # semantic checks assume schema-valid documents
        check_semantics(root, docs, rep)
        if repo is not None:
            check_repo(repo, docs, rep)
    check_placeholders(docs, rep)
    return rep


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--root", type=Path, default=DEFAULT_ROOT, help="directory containing index.yaml (default: architecture/)")
    ap.add_argument("--repo", type=Path, help="code repository to verify paths, evidence and revision against")
    ap.add_argument("--strict", action="store_true", help="treat warnings (incl. placeholders) as errors")
    ap.add_argument("--json", action="store_true", help="machine-readable output")
    args = ap.parse_args(argv)

    if not (args.root / SCHEMA_FILE).is_file():
        print(f"error: {args.root / SCHEMA_FILE} not found", file=sys.stderr)
        return 2
    if args.repo is not None and not args.repo.is_dir():
        print(f"error: --repo {args.repo} is not a directory", file=sys.stderr)
        return 2

    rep = run(args.root, args.repo)
    if args.json:
        print(json.dumps([f.__dict__ for f in rep.findings], indent=2))
    else:
        for f in rep.findings:
            print(f)
        print(f"{len(rep.errors)} error(s), {len(rep.warnings)} warning(s)")
        if args.repo is None:
            print("note: --repo not given; paths, evidence and revision were not verified")
    failed = bool(rep.errors) or (args.strict and bool(rep.warnings))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
