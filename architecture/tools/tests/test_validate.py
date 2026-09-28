"""Tests for validate.py. Every case starts from the shipped archmap v2 template."""
from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import validate  # noqa: E402

ARCH = Path(__file__).resolve().parents[2]
FIXTURE = Path(__file__).resolve().parent / "fixtures" / "model"


@pytest.fixture()
def model(tmp_path: Path) -> Path:
    dst = tmp_path / "architecture"
    dst.mkdir()
    shutil.copy2(ARCH / "archmap.schema.json", dst / "archmap.schema.json")
    shutil.copytree(FIXTURE, dst, dirs_exist_ok=True)
    return dst


def edit(model: Path, rel: str, old: str, new: str) -> None:
    p = model / rel
    text = p.read_text()
    assert old in text, f"{old!r} not in {rel}"
    p.write_text(text.replace(old, new, 1))


def errors(model: Path, repo: Path | None = None) -> list[str]:
    return [str(f) for f in validate.run(model, repo).errors]


def warnings(model: Path, repo: Path | None = None) -> list[str]:
    return [str(f) for f in validate.run(model, repo).warnings]


def has_error(model: Path, needle: str, repo: Path | None = None) -> bool:
    return any(needle in e for e in errors(model, repo))


def has_warning(model: Path, needle: str, repo: Path | None = None) -> bool:
    return any(needle in w for w in warnings(model, repo))


# ---- baseline -------------------------------------------------------------


def test_template_is_valid_with_placeholder_warnings(model):
    rep = validate.run(model)
    assert rep.errors == []
    assert rep.warnings and all("placeholder" in w.message or "has no relations" in w.message for w in rep.warnings)


def test_strict_fails_on_placeholders(model):
    assert validate.main(["--root", str(model), "--strict"]) == 1
    assert validate.main(["--root", str(model)]) == 0


# ---- yaml / schema --------------------------------------------------------


def test_duplicate_yaml_key_rejected(model):
    edit(model, "units/placeholder-db.yaml", "type: database", "type: database\ntype: storage")
    assert has_error(model, "YAML error")


def test_yaml_11_booleans_stay_strings(model):
    edit(model, "units/placeholder-db.yaml", 'tech: ["TODO"]', "tech: [no, on]")
    assert errors(model) == []


def test_unquoted_date_rejected_with_hint(model):
    edit(model, "index.yaml", 'verified_on: "1970-01-01"', "verified_on: 1970-01-01")
    assert has_error(model, "quote dates")


def test_unknown_key_rejected(model):
    edit(model, "units/placeholder-db.yaml", "type: database", "type: database\ninvented: 1")
    assert has_error(model, "invented")


def test_extension_key_allowed(model):
    edit(model, "units/placeholder-db.yaml", "type: database", "type: database\nx-owner: team-a")
    assert errors(model) == []


def test_confirmed_relation_requires_evidence(model):
    edit(
        model,
        "units/placeholder-api.yaml",
        ', evidence: ["TODO/path/to/unit/handler/file:1"], confidence: confirmed}',
        ", confidence: confirmed}",
    )
    assert has_error(model, "evidence")


def test_confirmed_invariant_requires_evidence(model):
    edit(model, "units/placeholder-api.yaml", "confidence: inferred\n\ncomponents:", "confidence: confirmed\n\ncomponents:")
    assert has_error(model, "evidence")


@pytest.mark.parametrize("bad", ["/abs/path", "../up", "with space", "a\\b"])
def test_bad_paths_rejected(model, bad):
    edit(model, "units/placeholder-db.yaml", '"TODO/path/to/migrations-or-schema/"', f"'{bad}'")
    assert has_error(model, "paths")


def test_multiline_summary_rejected(model):
    edit(
        model,
        "index.yaml",
        'summary: "TODO: primary datastore; state which unit is its only writer."',
        'summary: "TODO: primary datastore\\nsecond line"',
    )
    assert has_error(model, "summary")


def test_unit_does_not_duplicate_catalogue_summary(model):
    edit(model, "units/placeholder-db.yaml", "type: database", 'summary: "duplicated summary is deliberately forbidden"\ntype: database')
    assert has_error(model, "summary")


def test_import_relation_kind_is_not_manually_modelled(model):
    edit(model, "units/placeholder-api.yaml", "kind: calls", "kind: imports")
    assert has_error(model, "imports")


# ---- semantics ------------------------------------------------------------


def test_unknown_reference_with_hint(model):
    edit(model, "units/placeholder-api.yaml", "to: placeholder-db,", "to: placeholder-dbb,")
    assert has_error(model, "did you mean: placeholder-db")


def test_relation_must_live_with_its_source(model):
    edit(
        model,
        "units/placeholder-db.yaml",
        "tech:",
        "relations:\n  - {from: placeholder-api, to: placeholder-db, kind: writes, confidence: inferred}\ntech:",
    )
    assert has_error(model, "owns its source")


def test_external_source_only_in_index(model):
    edit(
        model,
        "units/placeholder-api.yaml",
        "relations:\n",
        "relations:\n  - {from: placeholder-user, to: placeholder-api, kind: calls, confidence: inferred}\n",
    )
    assert has_error(model, "owns its source")


def test_local_reference_rejected_in_flow(model):
    edit(model, "flows/placeholder-happy-path.yaml", "to: placeholder-api.placeholder-handler, action", "to: .placeholder-handler, action")
    assert has_error(model, "only allowed inside a unit file")


def test_duplicate_relation_rejected(model):
    line = '  - {from: .placeholder-service, to: .placeholder-repository, kind: calls, via: "in-process", evidence: ["TODO/path/to/unit/service/file:1"], confidence: confirmed}\n'
    edit(model, "units/placeholder-api.yaml", "relations:\n", "relations:\n" + line)
    assert has_error(model, "duplicate relation")


def test_self_relation_rejected(model):
    edit(model, "units/placeholder-api.yaml", "to: .placeholder-service, kind: calls", "to: .placeholder-handler, kind: calls")
    assert has_error(model, "same element")


def test_file_must_match_id(model):
    edit(model, "units/placeholder-db.yaml", "id: placeholder-db", "id: placeholder-other")
    assert has_error(model, "must equal the file name")


def test_orphan_file_rejected(model):
    shutil.copy(model / "units/placeholder-db.yaml", model / "units/stray.yaml")
    edit(model, "units/stray.yaml", "id: placeholder-db", "id: stray")
    assert has_error(model, "not listed")


def test_missing_top_level_notes_file_rejected(model):
    (model / "notes/placeholder-api.md").unlink()
    assert has_error(model, "does not exist")


def test_missing_external_notes_file_rejected(model):
    edit(model, "index.yaml", "type: system\n    summary:", 'type: system\n    notes: "notes/missing.md"\n    summary:')
    assert has_error(model, "externals.placeholder-payment-gateway.notes")


def test_id_shared_by_external_and_unit(model):
    edit(model, "index.yaml", "  placeholder-user:\n", "  placeholder-db:\n")
    assert has_error(model, "both an external and a unit")


def test_unknown_view_element_rejected(model):
    edit(model, "views/placeholder-core.yaml", "  - placeholder-db", "  - placeholder-dbb")
    assert has_error(model, "unknown element")


def test_unknown_view_flow_rejected(model):
    edit(
        model,
        "views/placeholder-core.yaml",
        "kind: structure\nelements:",
        "kind: flow\nflow: placeholder-missing-flow\nelements:",
    )
    assert has_error(model, "unknown flow")


def test_inferred_flow_step_without_relation_does_not_warn(model):
    edit(
        model,
        "flows/placeholder-happy-path.yaml",
        "to: placeholder-api.placeholder-handler, action:",
        "to: placeholder-db, action:",
    )
    assert not has_warning(model, "no declared relation")


def test_confirmed_flow_step_without_relation_warns(model):
    edit(
        model,
        "flows/placeholder-happy-path.yaml",
        'to: placeholder-api.placeholder-handler, action: "TODO: sends the request", confidence: inferred',
        'to: placeholder-db, action: "TODO: sends the request", evidence: ["TODO/path/to/unit/handler/file:1"], confidence: confirmed',
    )
    assert has_warning(model, "confirmed step has no declared relation")


# ---- repo checks ----------------------------------------------------------


@pytest.fixture()
def repo(tmp_path: Path) -> Path:
    r = tmp_path / "code"
    (r / "svc").mkdir(parents=True)
    (r / "svc/a.go").write_text("line1\nline2\nline3\n")
    git = lambda *a: subprocess.run(["git", "-C", str(r), *a], check=True, capture_output=True, text=True)  # noqa: E731
    git("init", "-q")
    git("-c", "user.name=t", "-c", "user.email=t@t", "add", ".")
    git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
    return r


def head(repo: Path) -> str:
    return subprocess.run(["git", "-C", str(repo), "rev-parse", "--short", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()


def real_model(model: Path, repo: Path) -> str:
    """Point every document at the same real revision and real paths."""
    rev = head(repo)
    for p in model.rglob("*.yaml"):
        text = p.read_text()
        text = text.replace('revision: "0000000"', f'revision: "{rev}"')
        text = text.replace('branch: "TODO"', 'branch: "master"')
        for old in (
            "TODO/path/to/unit/handler/file",
            "TODO/path/to/unit/service/file",
            "TODO/path/to/unit/repository/file",
        ):
            text = text.replace(old, "svc/a.go")
        for old in (
            "TODO/path/to/unit/handler/",
            "TODO/path/to/unit/service/",
            "TODO/path/to/unit/repository/",
            "TODO/path/to/unit/",
            "TODO/path/to/migrations-or-schema/",
        ):
            text = text.replace(old, "svc/")
        text = text.replace("TODO/path/to/main-file", "svc/a.go")
        p.write_text(text)
    return rev


def test_repo_checks_pass_on_real_paths(model, repo):
    real_model(model, repo)
    assert errors(model, repo) == []


def test_missing_path_at_recorded_revision(model, repo):
    real_model(model, repo)
    edit(model, "units/placeholder-db.yaml", 'paths: ["svc/"]', 'paths: ["missing/"]')
    assert has_error(model, "does not exist at recorded revision", repo)


def test_missing_evidence_file_at_recorded_revision(model, repo):
    real_model(model, repo)
    edit(model, "units/placeholder-api.yaml", "svc/a.go:1", "svc/missing.go:1")
    assert has_error(model, "does not exist at recorded revision", repo)


def test_evidence_line_past_eof_at_recorded_revision(model, repo):
    real_model(model, repo)
    edit(model, "units/placeholder-api.yaml", "svc/a.go:1", "svc/a.go:99")
    assert has_error(model, "past end of file", repo)


def test_evidence_is_checked_at_recorded_revision_not_head(model, repo):
    old_rev = real_model(model, repo)
    (repo / "svc/a.go").write_text("\n".join(f"line{i}" for i in range(1, 121)) + "\n")
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", "add", "."], check=True)
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "grow file"], check=True)
    edit(model, "units/placeholder-api.yaml", "svc/a.go:1", "svc/a.go:99")
    assert old_rev != head(repo)
    assert has_error(model, "past end of file at", repo)


def test_unknown_document_revision(model, repo):
    real_model(model, repo)
    edit(model, "flows/placeholder-happy-path.yaml", f'revision: "{head(repo)}"', 'revision: "deadbee"')
    assert has_error(model, "commit deadbee not found", repo)


def test_stale_revision_warns_per_document(model, repo):
    real_model(model, repo)
    (repo / "svc/b.go").write_text("x\n")
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", "add", "."], check=True)
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "more"], check=True)
    rep = validate.run(model, repo)
    stale = [w for w in rep.warnings if "behind HEAD" in w.message]
    assert len(stale) == 5  # index + 2 units + flow + view


def test_unknown_source_branch_warns(model, repo):
    real_model(model, repo)
    edit(model, "index.yaml", 'branch: "master"', 'branch: "definitely-not-a-branch"')
    assert has_warning(model, "not resolvable", repo)


def test_planned_unit_may_reference_nonexistent_paths(model, repo):
    real_model(model, repo)
    edit(model, "units/placeholder-db.yaml", "status: active", "status: planned")
    edit(model, "units/placeholder-db.yaml", 'paths: ["svc/"]', 'paths: ["future/not-created/"]')
    assert not has_error(model, "future/not-created", repo)
