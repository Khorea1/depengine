"""Tests for validate.py. Every case starts from the shipped template, which must be valid."""
from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import validate  # noqa: E402

ARCH = Path(__file__).resolve().parents[2]


@pytest.fixture()
def model(tmp_path: Path) -> Path:
    dst = tmp_path / "architecture"
    shutil.copytree(ARCH, dst, ignore=shutil.ignore_patterns("tools", "__pycache__"))
    return dst


def edit(model: Path, rel: str, old: str, new: str) -> None:
    p = model / rel
    text = p.read_text()
    assert old in text, f"{old!r} not in {rel}"
    p.write_text(text.replace(old, new, 1))


def errors(model: Path, repo: Path | None = None) -> list[str]:
    return [str(f) for f in validate.run(model, repo).errors]


def has(model: Path, needle: str, repo: Path | None = None) -> bool:
    return any(needle in e for e in errors(model, repo))


# ---- baseline -------------------------------------------------------------


def test_template_is_valid_with_placeholder_warnings(model):
    rep = validate.run(model)
    assert rep.errors == []
    assert rep.warnings and all("placeholder" in w.message for w in rep.warnings)


def test_strict_fails_on_placeholders(model):
    assert validate.main(["--root", str(model), "--strict"]) == 1
    assert validate.main(["--root", str(model)]) == 0


# ---- yaml / schema --------------------------------------------------------


def test_duplicate_yaml_key_rejected(model):
    edit(model, "containers/placeholder-db.yaml", "type: database", "type: database\ntype: cache")
    assert has(model, "YAML error")


def test_yaml_11_booleans_stay_strings(model):
    edit(model, "containers/placeholder-db.yaml", 'tech: ["TODO"]', "tech: [no, on]")
    assert errors(model) == []  # 'no'/'on' are strings under YAML 1.2, so this is valid


def test_unquoted_date_rejected_with_hint(model):
    edit(model, "index.yaml", 'verified_on: "1970-01-01"', "verified_on: 1970-01-01")
    assert has(model, "quote dates")


def test_unknown_key_rejected(model):
    edit(model, "containers/placeholder-db.yaml", "type: database", "type: database\ninvented: 1")
    assert has(model, "invented")


def test_extension_key_allowed(model):
    edit(model, "containers/placeholder-db.yaml", "type: database", "type: database\nx-owner: team-a")
    assert errors(model) == []


def test_confirmed_relation_requires_evidence(model):
    edit(model, "containers/placeholder-api.yaml", ', evidence: ["TODO/path/to/container/handler/file:1"], confidence: confirmed}', ", confidence: confirmed}")
    assert has(model, "evidence")


@pytest.mark.parametrize("bad", ["/abs/path", "../up", "with space", "a\\b"])
def test_bad_paths_rejected(model, bad):
    edit(model, "containers/placeholder-db.yaml", '"TODO/path/to/migrations-or-schema/"', f"'{bad}'")
    assert has(model, "path")


def test_control_character_in_path_rejected(model):
    # inside a YAML double-quoted scalar, \x08 decodes to a backspace character
    edit(model, "containers/placeholder-db.yaml", '"TODO/path/to/migrations-or-schema/"', '"svc\\x08dir"')
    assert has(model, "path")


def test_multiline_summary_rejected(model):
    edit(model, "containers/placeholder-db.yaml", 'summary: "TODO: primary datastore; state which container is its only writer."', 'summary: "TODO: primary datastore\\nsecond line"')
    assert has(model, "summary")


# ---- semantics ------------------------------------------------------------


def test_unknown_reference_with_hint(model):
    edit(model, "containers/placeholder-api.yaml", "to: placeholder-db,", "to: placeholder-dbb,")
    assert has(model, "did you mean: placeholder-db")


def test_relation_must_live_with_its_source(model):
    edit(model, "containers/placeholder-db.yaml", "tech:", "relations:\n  - {from: placeholder-api, to: placeholder-db, kind: writes, confidence: inferred}\ntech:")
    assert has(model, "owns its source")


def test_external_source_only_in_index(model):
    edit(model, "containers/placeholder-api.yaml", "relations:\n", "relations:\n  - {from: placeholder-user, to: placeholder-api, kind: calls, confidence: inferred}\n")
    assert has(model, "owns its source")


def test_local_reference_rejected_in_flow(model):
    edit(model, "flows/placeholder-happy-path.yaml", "to: placeholder-api.placeholder-handler, action", "to: .placeholder-handler, action")
    assert has(model, "only allowed inside a container file")


def test_duplicate_relation_rejected(model):
    line = "  - {from: .placeholder-service, to: .placeholder-repository, kind: calls, via: \"in-process\", evidence: [\"TODO/path/to/container/service/file:1\"], confidence: confirmed}\n"
    edit(model, "containers/placeholder-api.yaml", "relations:\n", "relations:\n" + line)
    assert has(model, "duplicate relation")


def test_self_relation_rejected(model):
    edit(model, "containers/placeholder-api.yaml", "to: .placeholder-service, kind: calls", "to: .placeholder-handler, kind: calls")
    assert has(model, "same element")


def test_summary_must_match_index(model):
    edit(model, "containers/placeholder-db.yaml", 'summary: "TODO: primary datastore; state', 'summary: "TODO: drifted datastore; state')
    assert has(model, "keep them identical")


def test_file_must_match_id(model):
    edit(model, "containers/placeholder-db.yaml", "id: placeholder-db", "id: placeholder-other")
    assert has(model, "must equal the file name")


def test_orphan_file_rejected(model):
    shutil.copy(model / "containers/placeholder-db.yaml", model / "containers/stray.yaml")
    edit(model, "containers/stray.yaml", "id: placeholder-db", "id: stray")
    assert has(model, "not listed")


def test_missing_notes_file_rejected(model):
    (model / "notes/placeholder-api.md").unlink()
    assert has(model, "does not exist")


def test_id_shared_by_external_and_container(model):
    edit(model, "index.yaml", "  placeholder-user:\n", "  placeholder-db:\n")
    assert has(model, "both an external and a container")


# ---- repo checks ----------------------------------------------------------


@pytest.fixture()
def repo(tmp_path: Path) -> Path:
    r = tmp_path / "code"
    (r / "svc").mkdir(parents=True)
    (r / "svc/a.go").write_text("line1\nline2\nline3\n")
    git = lambda *a: subprocess.run(["git", "-C", str(r), *a], check=True, capture_output=True)  # noqa: E731
    git("init", "-q")
    git("-c", "user.name=t", "-c", "user.email=t@t", "add", ".")
    git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
    return r


def real_model(model: Path, repo: Path) -> None:
    """Point the template at real files so repo checks can be exercised in isolation."""
    rev = subprocess.run(["git", "-C", str(repo), "rev-parse", "--short", "HEAD"], capture_output=True, text=True).stdout.strip()
    edit(model, "index.yaml", '"0000000"', f'"{rev}"')
    for rel in ("containers/placeholder-api.yaml", "containers/placeholder-db.yaml", "flows/placeholder-happy-path.yaml"):
        text = (model / rel).read_text()
        for old in ("TODO/path/to/container/handler/file", "TODO/path/to/container/service/file", "TODO/path/to/container/repository/file"):
            text = text.replace(old, "svc/a.go")
        for old in ("TODO/path/to/container/handler/", "TODO/path/to/container/service/", "TODO/path/to/container/repository/", "TODO/path/to/container/", "TODO/path/to/migrations-or-schema/"):
            text = text.replace(old, "svc/")
        text = text.replace("TODO/path/to/main-file", "svc/a.go")
        (model / rel).write_text(text)


def test_repo_checks_pass_on_real_paths(model, repo):
    real_model(model, repo)
    assert errors(model, repo) == []


def test_missing_evidence_file(model, repo):
    real_model(model, repo)
    edit(model, "containers/placeholder-api.yaml", "svc/a.go:1", "svc/missing.go:1")
    assert has(model, "does not exist in the repo", repo)


def test_evidence_line_past_eof(model, repo):
    real_model(model, repo)
    edit(model, "containers/placeholder-api.yaml", "svc/a.go:1", "svc/a.go:99")
    assert has(model, "past end of file", repo)


def test_unknown_revision(model, repo):
    real_model(model, repo)
    subprocess.run(["sed", "-i", "s/revision: \"[0-9a-f]*\"/revision: \"deadbee\"/", str(model / "index.yaml")], check=True)
    assert has(model, "not found", repo)


def test_stale_revision_warns(model, repo):
    real_model(model, repo)
    (repo / "svc/b.go").write_text("x\n")
    git = lambda *a: subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", *a], check=True, capture_output=True)  # noqa: E731
    git("add", ".")
    git("commit", "-qm", "more")
    assert any("behind HEAD" in w.message for w in validate.run(model, repo).warnings)
