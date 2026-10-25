#!/usr/bin/env python3
"""Run the recorded benchmark matrix serially, alternating variant order.

Build benchmark binaries with build_variants.py first. No build is performed here.
Defaults write new results under <repo>/_declaration-perf, outside archived evidence.
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
    cli.add_argument("--workspace", type=Path, help="directory containing variants, variants-22-24 and variants-parser")
    cli.add_argument("--output-dir", type=Path, help="default: <workspace>/matrix")
    cli.add_argument("--rounds", type=int, default=6)
    cli.add_argument("--groups", nargs="+", choices=["eligibility", "declaration", "classes", "emitter-retention", "metadata", "checker-lease", "parameters", "parser", "boundary"])
    cli.add_argument("--overwrite", action="store_true", help="replace existing result files in output directory")
    args = cli.parse_args()
    if args.rounds < 1:
        cli.error("--rounds must be positive")
    repo = args.repo.resolve()
    root = (args.workspace or repo / "_declaration-perf").resolve()
    out = (args.output_dir or root / "matrix").resolve()
    env = dict(os.environ, GOMAXPROCS="1", GOGC="100")
    old = lambda n: root / "variants" / f"variant{n:02d}.test"
    new = lambda n: root / "variants-22-24" / f"variant{n:02d}.test"
    parser = lambda n: root / "variants-parser" / f"variant{n:02d}.test"
    groups = [
     ('eligibility', [('06',old(6)),('24',new(24))], [('requests','^BenchmarkDeclarationEligibility$','1s'),('heap','^BenchmarkDeclarationEligibilityRetainedHeap$','1x')]),
     ('declaration', [('06',old(6)),('24',new(24))], [('work','^BenchmarkDeclaration$','1s'),('heap','^BenchmarkDeclarationRetainedHeap$','1x')]),
     ('classes', [('22',new(22)),('23',new(23)),('24',new(24))], [('work','^BenchmarkDeclarationClasses$','1s')]),
     ('emitter-retention', [('12',old(12)),('13',old(13))], [('heap','^BenchmarkEmitCompletedWorkerRetention$','1x')]),
     ('metadata', [('14',old(14)),('15',old(15))], [('work','^BenchmarkDeclaration$/AnnotatedTS_1/WarmEmit$','1s')]),
     ('checker-lease', [('19',old(19)),('20',old(20))], [('work','^BenchmarkDeclaration$/AnnotatedTS_1/WarmEmit$','1s')]),
     ('parameters', [('10',old(10)),('11',old(11))], [('work','^BenchmarkDeclaration$/(AnnotatedTS_200|JSCommonJS)/(WarmDiagnosticsUncached|WarmEmit)$','1s')]),
     ('parser', [('20',parser(20)),('21',parser(21))], [('work','^BenchmarkParse$','500ms'),('heap','^BenchmarkReturnedParserScannerRetention$','1x')]),
     ('boundary', [('24',new(24))], [('work','^BenchmarkDeclarationBoundary$','1s'),('heap','^BenchmarkDeclarationBoundaryRetainedHeap$','1x')]),
    ]
    if args.groups:
        groups = [group for group in groups if group[0] in args.groups]
    manifest = []
    for label, variants, workloads in groups:
        for name, binary in variants:
            if not binary.is_file():
                cli.error(f"Missing binary {binary}")
            digest = hashlib.sha256(binary.read_bytes()).hexdigest()
            for workload, pattern, bt in workloads:
                filename = f"{label}-{name}-{workload}.txt"
                if (out / filename).exists() and not args.overwrite:
                    cli.error(f"Result already exists: {out / filename}; choose another output or --overwrite")
                manifest.append(dict(group=label, variant=name, workload=workload, binary=str(binary), binary_sha256=digest, pattern=pattern, benchtime=bt, file=filename))
    if (out / "run-manifest.json").exists() and not args.overwrite:
        cli.error(f"Manifest already exists: {out / 'run-manifest.json'}; use a new output directory or --overwrite")
    out.mkdir(parents=True, exist_ok=True)
    for entry in manifest:
        (out / entry["file"]).write_text("")
    (out / "run-manifest.json").write_text(json.dumps(dict(rounds=args.rounds, environment={"GOMAXPROCS": "1", "GOGC": "100"}, runs=manifest), indent=2) + "\n")
    started = time.monotonic()
    for label, variants, workloads in groups:
        for round_index in range(args.rounds):
            # Preserve recorded rotation/reversal; pairs alternate explicitly.
            offset = round_index % len(variants)
            order = variants[offset:] + variants[:offset]
            if round_index % 2:
                order = list(reversed(order))
            if len(variants) == 2:
                order = variants if round_index % 2 == 0 else list(reversed(variants))
            for name, binary in order:
                for workload, pattern, bt in workloads:
                    command = [str(binary), "-test.run=^$", "-test.bench=" + pattern, "-test.benchtime=" + bt, "-test.benchmem", "-test.count=1", "-test.timeout=10m"]
                    result = subprocess.run(command, cwd=repo / "tsc", env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
                    with (out / f"{label}-{name}-{workload}.txt").open("a") as stream:
                        stream.write(result.stdout)
                    if result.returncode:
                        raise SystemExit(result.stdout)
            print(f"{label}: round {round_index + 1}/{args.rounds}, {time.monotonic() - started:.1f}s total", flush=True)
    print("All comparisons complete", flush=True)


if __name__ == "__main__":
    main()
