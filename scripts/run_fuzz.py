#!/usr/bin/env python3
"""Validate the fuzz target manifest and run every target for a bounded time."""

import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "scripts/fuzz-targets.txt"
INVENTORY = Path(__file__).resolve().parent / "fuzz_inventory.go"


def discover_targets(root: Path) -> set[tuple[str, str]]:
    directories = subprocess.run(
        ["go", "list", "-f", "{{.Dir}}", "./..."],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    targets = set()
    for directory in directories:
        relative = Path(os.path.relpath(directory, root))
        package = "." if str(relative) == "." else f"./{relative}"
        output = subprocess.run(
            ["go", "test", "-list=^Fuzz", "-run=^$", package],
            cwd=root,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        for line in output.splitlines():
            name = line.strip()
            if name.startswith("Fuzz") and name.isidentifier():
                targets.add((package, name))
    return targets



def discover_declared_targets(root: Path) -> set[tuple[str, str]]:
    output = subprocess.run(
        ["go", "run", str(INVENTORY)],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    targets = set()
    for line in output.splitlines():
        fields = line.split()
        if len(fields) != 2:
            raise ValueError(f"invalid fuzz inventory line: {line!r}")
        targets.add((fields[0], fields[1]))
    return targets

def read_manifest(path: Path) -> list[tuple[str, str]]:
    entries = []
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = line.partition("#")[0].strip()
        if not line:
            continue
        fields = line.split()
        if len(fields) != 2 or not (fields[0] == "." or fields[0].startswith("./")) or not fields[1].startswith("Fuzz"):
            raise ValueError(f"{path}:{number}: expected '<package> <FuzzTarget>'")
        entries.append((fields[0], fields[1]))
    return entries


def validate(root: Path, manifest: Path) -> list[tuple[str, str]]:
    entries = read_manifest(manifest)
    if len(entries) != len(set(entries)):
        raise ValueError(f"{manifest}: duplicate fuzz target entry")
    declared = discover_declared_targets(root)
    runnable = discover_targets(root)
    if not declared and not entries:
        raise ValueError("no fuzz targets discovered")
    listed = set(entries)
    missing = sorted(declared - listed)
    stale = sorted(listed - declared)
    inventory_gap = sorted(runnable - declared)
    if missing or stale or inventory_gap:
        details = []
        if missing:
            details.append("unlisted fuzz targets: " + ", ".join(f"{pkg} {name}" for pkg, name in missing))
        if stale:
            details.append("missing or renamed targets: " + ", ".join(f"{pkg} {name}" for pkg, name in stale))
        if inventory_gap:
            details.append("runtime targets missing from static inventory: " + ", ".join(f"{pkg} {name}" for pkg, name in inventory_gap))
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
    runnable = discover_targets(ROOT)
    for package, target in entries:
        if (package, target) not in runnable:
            print(f"==> {package} {target} (not runnable on this host; inventory only)", flush=True)
            continue
        print(f"==> {package} {target} ({duration})", flush=True)
        result = subprocess.run([
            "go", "test", package, "-run=^$", f"-fuzz=^{target}$", f"-fuzztime={duration}"
        ], cwd=ROOT)
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
