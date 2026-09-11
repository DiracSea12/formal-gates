#!/usr/bin/env python3
"""Parse QA helper syntax without importing/sourcing the inspected script."""
import argparse
import ast
import pathlib
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="+", type=pathlib.Path)
    args = parser.parse_args()
    failed = False
    for path in args.paths:
        try:
            if path.suffix == ".py":
                ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
            elif path.suffix == ".sh":
                # Noninteractive POSIX syntax checking does not source the file.
                result = subprocess.run(["sh", "-n", str(path.resolve())], capture_output=True, text=True)
                if result.returncode:
                    raise ValueError(result.stderr.strip())
            else:
                raise ValueError("unsupported static syntax checker; adapt explicitly")
            print(f"PASS syntax only: {path}")
        except (OSError, SyntaxError, ValueError) as error:
            failed = True
            print(f"FAIL {path}: {error}", file=sys.stderr)
    return int(failed)


if __name__ == "__main__":
    sys.exit(main())
