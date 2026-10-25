#!/usr/bin/env python3
"""Prepare/build this measurement matrix; --build is required to compile.

First: python build_all.py --mode adjacent --build
Later: python build_all.py --mode project --final-ref FINAL_COMMIT --project-test-ref HARNESS33_COMMIT --build
Builds are serial. This script never runs benchmarks.
"""

import argparse
import json
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parent
REFS = {
    "25": "refactor/25-editor-declaration-boundary-benchmarks",
    "26": "refactor/26-single-file-transpile-checker",
    "27": "refactor/27-preserve-negative-directory-dependencies",
    "28": "refactor/28-syntax-only-declaration-modifiers",
}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--mode", choices=["all", "adjacent", "project"], default="all")
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--final-ref")
    parser.add_argument("--project-test-ref")
    parser.add_argument("--repo", type=Path, default=ROOT.parent / "ts-small")
    parser.add_argument("--go", type=Path, default=ROOT.parent / "toolchain/go/bin/go")
    parser.add_argument("--gopath", type=Path, default=Path("/root/go"))
    args = parser.parse_args()
    if args.mode != "adjacent" and (not args.final_ref or not args.project_test_ref):
        parser.error("project builds require --final-ref and --project-test-ref")
    repo = args.repo.resolve()

    def resolve(ref):
        return subprocess.check_output(["/usr/bin/git", "-C", str(repo), "rev-parse", "--verify", ref + "^{commit}"], text=True).strip()

    commits = {name: resolve(ref) for name, ref in REFS.items()}
    if args.mode != "adjacent":
        commits["final"] = resolve(args.final_ref)
        commits["project_tests"] = resolve(args.project_test_ref)
    commands = []
    base = [sys.executable, str(ROOT / "build_variants.py"), "--repo", str(repo), "--go", str(args.go.resolve()), "--gopath", str(args.gopath.resolve()), "--base-ref", commits["25"]]
    if args.mode != "project":
        commands.append(base + ["--package", "transpile", "--test-ref", commits["26"], "--output-dir", str(ROOT / "variants-transpile"), "25=" + commits["25"], "26=" + commits["26"]])
        commands.append(base + ["--package", "compiler", "--test-ref", commits["28"], "--output-dir", str(ROOT / "variants-compiler"), "27=" + commits["27"], "28=" + commits["28"]])
    if args.mode != "adjacent":
        commands.append(base + [
            "--package", "project", "--test-ref", commits["25"], "--no-trimpath",
            "--test-override", "tsc/internal/project/declaration_boundary_bench_test.go=" + commits["project_tests"],
            "--omit-test-function", "TestEditorDeclarationBoundaryPhysicalOutputLifecycle",
            "--output-dir", str(ROOT / "variants-project"),
            "25=" + commits["25"], "final=" + commits["final"],
        ])
    if args.build:
        commands = [command + ["--build"] for command in commands]
    plan = {"mode": args.mode, "build": args.build, "repo": str(repo), "resolved_commits": commits, "commands": commands}
    (ROOT / ("build-plan-" + args.mode + ".json")).write_text(json.dumps(plan, indent=2) + "\n")
    for command in commands:
        subprocess.run(command, check=True)


if __name__ == "__main__":
    main()
