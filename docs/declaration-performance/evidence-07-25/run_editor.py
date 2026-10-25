#!/usr/bin/env python3
"""Run the editor benchmark arms in separate processes with alternating order.

Build project25.test separately; this script never compiles code. --runtime-ref
records the source-tree identity asserted by the caller for that binary.
"""
from pathlib import Path
import argparse
import os
import subprocess
import time
import json
import hashlib

HERE = Path(__file__).resolve().parent
DEFAULT_REPO = next((p for p in HERE.parents if (p / "tsc/go.mod").is_file()), Path.cwd())


def main():
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("--repo", type=Path, default=DEFAULT_REPO)
    cli.add_argument("--workspace", type=Path)
    cli.add_argument("--binary", type=Path, help="default: <workspace>/project25.test")
    cli.add_argument("--output-dir", type=Path, help="default: <workspace>/editor")
    cli.add_argument("--runtime-ref", default="refactor/25-editor-declaration-boundary-benchmarks")
    cli.add_argument("--rounds", type=int, default=6)
    cli.add_argument("--overwrite", action="store_true")
    args = cli.parse_args()
    if args.rounds < 1:
        cli.error("--rounds must be positive")
    repo = args.repo.resolve()
    root = (args.workspace or repo / "_declaration-perf").resolve()
    binary = (args.binary or root / "project25.test").resolve()
    out = (args.output_dir or root / "editor").resolve()
    if not binary.is_file():
        cli.error(f"Missing binary {binary}")
    revision = None
    for candidate in [args.runtime_ref, "refs/remotes/origin/" + args.runtime_ref]:
        result = subprocess.run(["git", "-C", str(repo), "rev-parse", "--verify", candidate + "^{commit}"], capture_output=True, text=True)
        if result.returncode == 0:
            revision = result.stdout.strip()
            break
    if revision is None:
        cli.error("Missing runtime ref; fetch the fork branches first")
    tree = subprocess.check_output(["git", "-C", str(repo), "rev-parse", revision + "^{tree}"], text=True).strip()
    modes = ["SourceDependencies", "Declarations"]
    files = [f"{mode}-{kind}.txt" for mode in modes for kind in ["work", "heap"]]
    for filename in files:
        if (out / filename).exists() and not args.overwrite:
            cli.error(f"Result already exists: {out / filename}; choose another output or --overwrite")
    if (out / "run-manifest.json").exists() and not args.overwrite:
        cli.error(f"Manifest already exists: {out / 'run-manifest.json'}; use a new output directory or --overwrite")
    out.mkdir(parents=True, exist_ok=True)
    for filename in files:
        (out / filename).write_text("")
    manifest = dict(binary=str(binary), binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), runtime_ref=args.runtime_ref, runtime_revision=revision, runtime_tree=tree, GOMAXPROCS=1, GOGC=100, rounds=args.rounds, work_benchtime="1s", heap_benchtime="1x", order="alternating arms in separate processes")
    (out / "run-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    env = dict(os.environ, GOMAXPROCS="1", GOGC="100")
    start = time.monotonic()
    for round_index in range(args.rounds):
        order = modes if round_index % 2 == 0 else list(reversed(modes))
        for mode in order:
            for kind, bench, bt in [("work", "BenchmarkEditorDeclarationBoundary", "1s"), ("heap", "BenchmarkEditorDeclarationBoundaryRetainedHeap", "1x")]:
                command = [str(binary), "-test.run=^$", "-test.bench=^" + bench + "$/" + mode + "$", "-test.benchtime=" + bt, "-test.benchmem", "-test.count=1", "-test.timeout=10m"]
                result = subprocess.run(command, cwd=repo / "tsc", env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
                with (out / f"{mode}-{kind}.txt").open("a") as stream:
                    stream.write(result.stdout)
                if result.returncode:
                    raise SystemExit(result.stdout)
        print(f"Editor round {round_index + 1}/{args.rounds}: {time.monotonic() - start:.1f}s", flush=True)


if __name__ == "__main__":
    main()
