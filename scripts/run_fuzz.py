#!/usr/bin/env python3
"""Validate the fuzz target manifest and run every target for a bounded time."""

import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "scripts/fuzz-targets.txt"
FUZZ_DECL = re.compile(r"^func (Fuzz\w+)\s*\(f \*testing\.F\)", re.MULTILINE)


def discover_targets(root: Path) -> set[tuple[str, str]]:
    targets = set()
    files = subprocess.run(
        ["git", "-C", str(root), "ls-files", "--", "*_test.go"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    for filename in files:
        path = root / filename
        if not path.is_file():
            continue
        package = "./" + str(Path(filename).parent)
        for name in FUZZ_DECL.findall(path.read_text(encoding="utf-8")):
            targets.add((package, name))
    return targets


def read_manifest(path: Path) -> list[tuple[str, str]]:
    entries = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = line.partition("#")[0].strip()
        if not line:
            continue
        fields = line.split()
        if len(fields) != 2 or not fields[0].startswith("./") or not fields[1].startswith("Fuzz"):
            raise ValueError(f"{path}:{number}: expected '<package> <FuzzTarget>'")
        entries.append((fields[0], fields[1]))
    return entries


def validate(root: Path, manifest: Path) -> list[tuple[str, str]]:
    entries = read_manifest(manifest)
    if len(entries) != len(set(entries)):
        raise ValueError(f"{manifest}: duplicate fuzz target entry")
    discovered = discover_targets(root)
    listed = set(entries)
    missing = sorted(discovered - listed)
    stale = sorted(listed - discovered)
    if missing or stale:
        details = []
        if missing:
            details.append("unlisted fuzz targets: " + ", ".join(f"{pkg} {name}" for pkg, name in missing))
        if stale:
            details.append("missing or renamed targets: " + ", ".join(f"{pkg} {name}" for pkg, name in stale))
        raise ValueError("; ".join(details))
    return entries


def main() -> int:
    try:
        entries = validate(ROOT, MANIFEST)
    except (OSError, subprocess.CalledProcessError, ValueError) as error:
        print(f"fuzz target validation failed: {error}", file=sys.stderr)
        return 1

    duration = "10s"
    if len(sys.argv) == 2:
        duration = sys.argv[1]
    elif len(sys.argv) > 2:
        print("usage: scripts/run_fuzz.py [fuzztime]", file=sys.stderr)
        return 2
    for package, target in entries:
        print(f"==> {package} {target} ({duration})", flush=True)
        result = subprocess.run([
            "go", "test", package, "-run=^$", f"-fuzz=^{target}$", f"-fuzztime={duration}"
        ], cwd=ROOT)
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
