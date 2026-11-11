#!/usr/bin/env python3
"""Check source layout and the project's 1000-line file limit."""

from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
MAX_LINES = 1000


def main() -> int:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
        check=True,
        capture_output=True,
    )
    paths = sorted({Path(item.decode()) for item in result.stdout.split(b"\0") if item})
    failures = []
    largest = (0, "")
    checked = 0
    for relative in paths:
        path = ROOT / relative
        if not path.is_file():
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        checked += 1
        lines = len(text.splitlines())
        largest = max(largest, (lines, str(relative)))
        if lines > MAX_LINES:
            failures.append(f"{relative}: {lines} lines exceeds {MAX_LINES}")
        is_test = path.name.endswith("_test.go") or (path.name.startswith("test_") and path.suffix == ".py")
        if is_test and "tests" not in relative.parts:
            failures.append(f"{relative}: test file must live in a tests directory")
        if path.name == "go.mod":
            match = re.search(r"^module\s+(\S+)", text, re.MULTILINE)
            if not match or not (match[1] == "portexa" or match[1].startswith("portexa/")):
                failures.append(f"{relative}: module must use a project-scoped name")
        if relative.parts[:2] == ("packages", "mt4") and path.suffix == ".go":
            imports = re.findall(r'"(portexa(?:/[^"\s]+)?)"', text)
            if any(name != "portexa/mt4" and not name.startswith("portexa/mt4/") for name in imports):
                failures.append(f"{relative}: protocol module cannot import the host project")
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"structure ok: {checked} text files; largest {largest[1]} ({largest[0]}/{MAX_LINES} lines)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
