#!/usr/bin/env python3
"""Validate an archmap model.

Checks, in order:
  1. YAML 1.2 parse, duplicate keys rejected.
  2. JSON Schema (archmap.schema.json), selected by file location.
  3. Semantics: id/file consistency, reference resolution, relation ownership,
     view/flow references, duplicates and notes links.
  4. With --repo: each document's recorded revision is known; paths and evidence
     are checked at that revision (not at the current working tree); freshness is
     reported per document.
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
    return rel.split("/", 1)[0].removesuffix("s")


def _fmt_path(parts: Any) -> str:
    out = ""
    for p in parts:
        out += f"[{p}]" if isinstance(p, int) else (("." if out else "") + str(p))
    return out


def _discover(root: Path, rep: Report) -> list[str]:
    rels: list[str] = []
    if (root / "index.yaml").is_file():
        rels.append("index.yaml")
    else:
        rep.error("index.yaml", "", "missing: every model needs an index.yaml")
    for sub in ("units", "flows", "views"):
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
    try:
        schema = json.loads((root / SCHEMA_FILE).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        rep.error(SCHEMA_FILE, "", f"cannot load schema: {exc}")
        return {}

    yaml = _yaml()
    docs: dict[str, dict] = {}
    for rel in _discover(root, rep):
        try:
            data = yaml.load((root / rel).read_text(encoding="utf-8"))
        except (OSError, YAMLError) as exc:
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
    """Yield (file, where, relation, owner_unit_or_None)."""
    for rel, d in docs.items():
        owner = d["id"] if d["doc"] == "unit" else None
        if d["doc"] in ("index", "unit"):
            for i, relation in enumerate(d.get("relations", [])):
                yield rel, f"relations[{i}]", relation, owner


def _iter_steps(docs: dict[str, dict]) -> Iterator[tuple[str, str, dict]]:
    for rel, d in docs.items():
        if d["doc"] == "flow":
            for i, step in enumerate(d["steps"]):
                yield rel, f"steps[{i}]", step


def _iter_claims(docs: dict[str, dict]) -> Iterator[tuple[str, str, dict]]:
    for rel, d in docs.items():
        if d["doc"] != "unit":
            continue
        for i, claim in enumerate(d.get("invariants", [])):
            yield rel, f"invariants[{i}]", claim
        for cid, component in d.get("components", {}).items():
            for i, claim in enumerate(component.get("invariants", [])):
                yield rel, f"components.{cid}.invariants[{i}]", claim


def _iter_notes(docs: dict[str, dict]) -> Iterator[tuple[str, str, str]]:
    for rel, d in docs.items():
        if d.get("notes"):
            yield rel, "notes", d["notes"]
        if d["doc"] == "index":
            for eid, external in d.get("externals", {}).items():
                if external.get("notes"):
                    yield rel, f"externals.{eid}.notes", external["notes"]


def check_semantics(root: Path, docs: dict[str, dict], rep: Report) -> dict[str, str]:
    """Return the element table {ref: kind}; empty if index.yaml is unusable."""
    idx = docs.get("index.yaml")
    if idx is None:
        return {}

    externals: dict = idx.get("externals", {})
    units: dict = idx["units"]
    flows: dict = idx.get("flows", {})
    views: dict = idx.get("views", {})

    for clash in sorted(set(externals) & set(units)):
        rep.error("index.yaml", f"externals.{clash}", "id is used by both an external and a unit")

    # index <-> detail files. Summaries intentionally live only in index.yaml.
    for kind_dir, doc_kind, catalogue in (
        ("units", "unit", units),
        ("flows", "flow", flows),
        ("views", "view", views),
    ):
        for item_id in catalogue:
            rel = f"{kind_dir}/{item_id}.yaml"
            if rel not in docs:
                if not (root / rel).is_file():
                    rep.error("index.yaml", f"{kind_dir}.{item_id}", f"listed but {rel} does not exist")
                continue
            d = docs[rel]
            if d["id"] != item_id:
                rep.error(rel, "id", f"id {d['id']!r} must equal the file name and index key {item_id!r}")
        for rel, d in docs.items():
            if d["doc"] == doc_kind and Path(rel).stem not in catalogue:
                rep.error(rel, "", f"file is not listed under '{kind_dir}' in index.yaml")

    # element table
    elements: dict[str, str] = {e: "external" for e in externals}
    elements.update({u: "unit" for u in units})
    for _, d in docs.items():
        if d["doc"] == "unit":
            for comp in d.get("components", {}):
                elements[f"{d['id']}.{comp}"] = "component"

    def resolve(ref: str, owner: str | None, file: str, where: str) -> str | None:
        full = ref
        if ref.startswith("."):
            if owner is None:
                rep.error(file, where, f"local reference {ref!r} is only allowed inside a unit file")
                return None
            full = owner + ref
        if full not in elements:
            hint = difflib.get_close_matches(full, list(elements), n=3, cutoff=0.6)
            suffix = f" (did you mean: {', '.join(hint)}?)" if hint else ""
            rep.error(file, where, f"unknown element {ref!r}{suffix}")
            return None
        return full

    def top(ref: str) -> str:
        return ref.split(".")[0]

    # relations
    seen: set[tuple[str, str, str]] = set()
    for file, where, relation, owner in _iter_relations(docs):
        src = resolve(relation["from"], owner, file, f"{where}.from")
        dst = resolve(relation["to"], owner, file, f"{where}.to")
        if src is None or dst is None:
            continue
        if src == dst:
            rep.error(file, where, "from and to are the same element")
        if owner is None:
            if elements[src] != "external":
                rep.error(file, f"{where}.from", f"{src!r} is not an external; put this relation in units/{top(src)}.yaml")
        elif src != owner and not src.startswith(owner + "."):
            home = "index.yaml" if elements[src] == "external" else f"units/{top(src)}.yaml"
            rep.error(file, f"{where}.from", f"relation must live in the file that owns its source; move it to {home}")
        key = (src, dst, relation["kind"])
        if key in seen:
            rep.error(file, where, f"duplicate relation {src} -{relation['kind']}-> {dst}")
        seen.add(key)

    # flows. Missing declared relations are useful warnings only for confirmed steps;
    # an inferred step is explicitly allowed to be a lead not yet present in the model.
    pairs = {(src, dst) for src, dst, _ in seen}
    top_pairs = {(top(src), top(dst)) for src, dst in pairs}
    for file, where, step in _iter_steps(docs):
        src = resolve(step["from"], None, file, f"{where}.from")
        dst = resolve(step["to"], None, file, f"{where}.to")
        if (
            src
            and dst
            and step["confidence"] == "confirmed"
            and (src, dst) not in pairs
            and (top(src), top(dst)) not in top_pairs
        ):
            rep.warn(file, where, f"confirmed step has no declared relation between {top(src)} and {top(dst)}")

    # views reference existing elements/flows but carry no layout semantics.
    for file, d in docs.items():
        if d["doc"] != "view":
            continue
        for i, ref in enumerate(d.get("elements", [])):
            resolve(ref, None, file, f"elements[{i}]")
        flow = d.get("flow")
        if flow is not None and flow not in flows:
            hint = difflib.get_close_matches(flow, list(flows), n=3, cutoff=0.6)
            suffix = f" (did you mean: {', '.join(hint)}?)" if hint else ""
            rep.error(file, "flow", f"unknown flow {flow!r}{suffix}")

    # notes links, including external.notes (previously easy to forget).
    for file, where, note in _iter_notes(docs):
        if not (root / note).is_file():
            rep.error(file, where, f"{note} does not exist")

    # Isolated top-level elements are often accidental and worth surfacing.
    for name, kind in elements.items():
        if kind == "component":
            continue
        connected = any(
            name == a or name == b or a.startswith(name + ".") or b.startswith(name + ".")
            for a, b in pairs
        )
        if not connected:
            rep.warn("index.yaml", name, f"{kind} {name!r} has no relations")
    return elements


# --------------------------------------------------------------------------- #
# repo checks
# --------------------------------------------------------------------------- #


def _git(repo: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["git", "-c", f"safe.directory={repo}", "-C", str(repo), *args], capture_output=True, text=True)


def _is_placeholder(value: str) -> bool:
    return any(pattern.search(value) for pattern in PLACEHOLDER_RES)


def check_repo(repo: Path, docs: dict[str, dict], rep: Report) -> None:
    commit_ok: dict[str, bool] = {}
    file_cache: dict[tuple[str, str], str | None] = {}

    def valid_commit(file: str, rev: str) -> bool:
        if _is_placeholder(rev):
            return False
        if rev in commit_ok:
            return commit_ok[rev]
        ok = _git(repo, "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}").returncode == 0
        commit_ok[rev] = ok
        if not ok:
            rep.error(file, "verified.revision", f"commit {rev} not found in {repo}")
        return ok

    # Advisory branch metadata is checked once. It is deliberately not the source
    # of freshness; each document's verified.revision is.
    idx = docs.get("index.yaml")
    if idx is not None:
        branch = idx.get("project", {}).get("source", {}).get("branch")
        if branch and not _is_placeholder(branch):
            if _git(repo, "rev-parse", "--verify", "--quiet", f"{branch}^{{commit}}").returncode != 0:
                rep.warn("index.yaml", "project.source.branch", f"branch/ref {branch!r} is not resolvable in {repo}")

    # Validate/freshness-check every document independently.
    for file, d in docs.items():
        rev = d["verified"]["revision"]
        if not valid_commit(file, rev):
            continue
        ancestor = _git(repo, "merge-base", "--is-ancestor", rev, "HEAD")
        if ancestor.returncode == 0:
            n = _git(repo, "rev-list", "--count", f"{rev}..HEAD").stdout.strip()
            if n.isdigit() and int(n) > 0:
                rep.warn(file, "verified.revision", f"document is {n} commit(s) behind HEAD; re-verify this document when relevant")
        else:
            rep.warn(file, "verified.revision", f"recorded revision {rev} is not an ancestor of HEAD")

    def object_exists(rev: str, path: str) -> bool:
        clean = path.rstrip("/") or "."
        if clean == ".":
            return True
        return _git(repo, "cat-file", "-e", f"{rev}:{clean}").returncode == 0

    def exists(file: str, where: str, rev: str, path: str) -> None:
        if valid_commit(file, rev) and not object_exists(rev, path):
            rep.error(file, where, f"path {path!r} does not exist at recorded revision {rev}")

    def read_at(file: str, rev: str, path: str) -> str | None:
        key = (rev, path)
        if key in file_cache:
            return file_cache[key]
        if not valid_commit(file, rev):
            file_cache[key] = None
            return None
        proc = _git(repo, "show", f"{rev}:{path}")
        if proc.returncode != 0:
            file_cache[key] = None
            return None
        file_cache[key] = proc.stdout
        return proc.stdout

    def check_evidence(file: str, where: str, rev: str, ev: str) -> None:
        path, _, span = ev.partition(":")
        text = read_at(file, rev, path)
        if text is None:
            rep.error(file, where, f"evidence file {path!r} does not exist at recorded revision {rev}")
            return
        if span:
            line_count = len(text.splitlines())
            end = int(span.split("-")[-1])
            if end > line_count:
                rep.error(file, where, f"evidence {ev!r} is past end of file at {rev} ({line_count} lines)")

    # Paths are checked at the same revision as their owning unit. Planned units
    # and components may legitimately point at paths that do not exist yet.
    for file, d in docs.items():
        if d["doc"] != "unit":
            continue
        rev = d["verified"]["revision"]
        unit_planned = d.get("status") == "planned"
        if not unit_planned:
            for i, path in enumerate(d["paths"]):
                exists(file, f"paths[{i}]", rev, path)
            for i, path in enumerate(d.get("entrypoints", [])):
                exists(file, f"entrypoints[{i}]", rev, path)
        roots = [p.rstrip("/") for p in d["paths"]]
        for cid, component in d.get("components", {}).items():
            component_planned = unit_planned or component.get("status") == "planned"
            if not component_planned:
                for i, path in enumerate(component["paths"]):
                    exists(file, f"components.{cid}.paths[{i}]", rev, path)
                for i, path in enumerate(component.get("entrypoints", [])):
                    exists(file, f"components.{cid}.entrypoints[{i}]", rev, path)
            for path in component["paths"]:
                clean = path.rstrip("/")
                if roots != ["."] and not any(clean == root or clean.startswith(root + "/") for root in roots):
                    rep.warn(file, f"components.{cid}.paths", f"{path!r} is outside all unit paths {d['paths']!r}")

    # Evidence is always read from the document's recorded revision, never HEAD.
    for file, where, relation, _ in _iter_relations(docs):
        rev = docs[file]["verified"]["revision"]
        for i, ev in enumerate(relation.get("evidence", [])):
            check_evidence(file, f"{where}.evidence[{i}]", rev, ev)
    for file, where, step in _iter_steps(docs):
        rev = docs[file]["verified"]["revision"]
        for i, ev in enumerate(step.get("evidence", [])):
            check_evidence(file, f"{where}.evidence[{i}]", rev, ev)
    for file, where, claim in _iter_claims(docs):
        rev = docs[file]["verified"]["revision"]
        for i, ev in enumerate(claim.get("evidence", [])):
            check_evidence(file, f"{where}.evidence[{i}]", rev, ev)


# --------------------------------------------------------------------------- #
# placeholders
# --------------------------------------------------------------------------- #


def _walk(node: Any, path: str = "") -> Iterator[tuple[str, str]]:
    if isinstance(node, dict):
        for key, value in node.items():
            sub = f"{path}.{key}" if path else str(key)
            yield sub, str(key)
            yield from _walk(value, sub)
    elif isinstance(node, list):
        for i, value in enumerate(node):
            yield from _walk(value, f"{path}[{i}]")
    elif isinstance(node, str):
        yield path, node


def check_placeholders(docs: dict[str, dict], rep: Report) -> None:
    """One warning per file, so a fresh template does not bury real findings."""
    for file, d in docs.items():
        hits = [where for where, value in _walk(d) if _is_placeholder(value)]
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
    ap.add_argument("--repo", type=Path, help="code repository to verify paths, evidence and per-document revisions against")
    ap.add_argument("--strict", action="store_true", help="treat warnings (including placeholders and staleness) as errors")
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
        for finding in rep.findings:
            print(finding)
        print(f"{len(rep.errors)} error(s), {len(rep.warnings)} warning(s)")
        if args.repo is None:
            print("note: --repo not given; paths, evidence and recorded revisions were not verified")
    failed = bool(rep.errors) or (args.strict and bool(rep.warnings))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
