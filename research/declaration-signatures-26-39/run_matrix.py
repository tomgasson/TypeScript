#!/usr/bin/env python3
"""Run six balanced rounds only with --run, after all builds/tests have stopped.

No compiler or test builds are started here. Binaries are checked against the
exact build manifests before execution. Each sample runs in a separate process.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time


ROOT = Path(__file__).resolve().parent


def groups():
    result = [
        {"name": "transpile", "arms": [("25", "transpile", "25", None), ("26", "transpile", "26", None)], "workloads": [("work", "^BenchmarkTranspile$", "1s")],
         "expected": {"BenchmarkTranspile/" + mode + "/Annotated_" + size for mode in ["Module", "Declaration"] for size in ["1", "200"]}},
        {"name": "modifiers", "arms": [("27", "compiler", "27", None), ("28", "compiler", "28", None)], "workloads": [("work", "^BenchmarkDeclarationClasses$", "1s")],
         "expected": {"BenchmarkDeclarationClasses/" + case + "/" + leaf for case in ["AnnotatedTS_1", "AnnotatedTS_200", "JSFullSignature_1", "JSFullSignature_200"] for leaf in ["WarmDiagnosticsUncached", "WarmEmit"]}},
        {"name": "source-overhead", "arms": [("25", "project", "25", "SourceDependencies"), ("final", "project", "final", "SourceDependencies")],
         "workloads": [("work", "^BenchmarkEditorDeclarationBoundary$/^{mode}$", "1s"), ("heap", "^BenchmarkEditorDeclarationBoundaryRetainedHeap$/^{mode}$", "1x")]},
        {"name": "consumer-reuse", "arms": [(mode, "project", "final", mode) for mode in ["SourceDependencies", "PreparedDeclarations", "AutomaticDeclarations"]],
         "workloads": [("work", "^BenchmarkEditorDeclarationBoundary$/^{mode}$", "1s"), ("heap", "^BenchmarkEditorDeclarationBoundaryRetainedHeap$/^{mode}$", "1x")]},
    ]
    for case in ["BodyOnly", "PublicSignature", "ProducerConfig", "TransitiveSignature"]:
        workloads = [("work", "^BenchmarkEditorDeclarationProducerChanges$/^" + case + "$/^{mode}$", "1s")]
        if case in ["BodyOnly", "PublicSignature"]:
            workloads.append(("heap", "^BenchmarkEditorDeclarationProducerRetainedHeap$/^" + case + "$/^{mode}$", "1x"))
        result.append({"name": "producer-" + case, "arms": [(mode, "project", "final", mode) for mode in ["SourceDependencies", "AutomaticDeclarations"]], "workloads": workloads})
    return result


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def balanced(arms, round_index):
    if len(arms) == 2:
        return arms if round_index % 2 == 0 else list(reversed(arms))
    rotated = arms[round_index % len(arms):] + arms[:round_index % len(arms)]
    return rotated if round_index % 2 == 0 else list(reversed(rotated))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true", help="explicitly start measurements")
    parser.add_argument("--group", action="append", choices=[group["name"] for group in groups()])
    parser.add_argument("--repo", type=Path, default=ROOT.parent / "ts-small")
    parser.add_argument("--output-dir", type=Path, default=ROOT / "matrix")
    parser.add_argument("--replace", action="store_true", help="replace existing output files for the selected groups")
    args = parser.parse_args()
    selected = [group for group in groups() if not args.group or group["name"] in args.group]
    if not args.run:
        for group in selected:
            print(group["name"] + ": " + ", ".join(arm[0] for arm in group["arms"]) + "; " + ", ".join(kind + "=" + duration for kind, _, duration in group["workloads"]))
        print("No benchmarks run. Pass --run only after all builds/tests finish.")
        return

    output = args.output_dir.resolve()
    # Even disjoint --group runs must not silently replace existing provenance.
    if (output / "run-manifest.json").exists() and not args.replace:
        raise RuntimeError("Refusing to replace existing run manifest without --replace; use another --output-dir: " + str(output))
    output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOMAXPROCS="1", GOGC="100")
    builds, checked = {}, set()
    runs = []
    for group in selected:
        for label, package, variant, mode in group["arms"]:
            if package not in builds:
                path = ROOT / ("variants-" + package) / "build-variants.json"
                builds[package] = {"path": str(path), "sha256": sha256(path), "manifest": json.loads(path.read_text())}
            build = builds[package]["manifest"]
            item = build["variants"][variant]
            binary = Path(item["binary"])
            if item["status"] != "built":
                raise RuntimeError("Variant is not built: " + str(binary))
            if binary not in checked:
                if sha256(binary) != item["binary_sha256"]:
                    raise RuntimeError("Binary differs from its build manifest: " + str(binary))
                checked.add(binary)
            for workload, template, duration in group["workloads"]:
                pattern = template.format(mode=mode)
                expected = sorted(group.get("expected", {pattern.replace("^", "").replace("$", "")}))
                filename = f"{group['name']}-{label}-{workload}.txt"
                path = output / filename
                if path.exists() and path.stat().st_size and not args.replace:
                    raise RuntimeError("Refusing to replace existing samples without --replace: " + str(path))
                runs.append({
                    "group": group["name"], "arm": label, "package": package, "variant": variant, "mode": mode,
                    "workload": workload, "pattern": pattern, "benchtime": duration, "expected_benchmarks": expected,
                    "binary": str(binary), "binary_sha256": item["binary_sha256"],
                    "revision": item["revision"], "tree": item["tree"],
                    "build_manifest": builds[package]["path"], "file": filename,
                })
    for run in runs:
        (output / run["file"]).write_text("")
    manifest = {
        "rounds": 6, "environment": {"GOMAXPROCS": "1", "GOGC": "100"},
        "cwd": str(args.repo.resolve() / "tsc"), "runs": runs, "builds": builds,
        "ordering": "Pairs alternate; three arms rotate and reverse across six rounds.",
        "fixture": {"producer_modules": 48, "locals_per_module": 256, "producer_files_including_barrel": 49, "retained_sessions": 4, "edits_per_retained_session": 16},
        "scope": {
            "automatic": "Per-loader singleflight output strings. A full consumer rebuild regenerates the complete producer; no persistent cross-snapshot extraction cache.",
            "consumer_reuse": "Generation is outside steady consumer-edit timing; normal Program cloning reuses declaration ASTs and existing owned output.",
            "producer_changes": "Producer edit, watched-file notification, regeneration when required, consumer diagnostics and background drain are all timed.",
            "read_counters": "Successful underlying producer/src ReadFile calls/bytes only. SnapshotFS can serve cached inputs during regeneration; these are not extraction calls or cache hit/miss counts.",
            "retention": "Post-GC marginal live heap, not RSS/peak. Prepared text is shared; automatic generated output and tracking state are owned separately per Session.",
            "source_control": "Same new benchmark functions on exact production25 and final runtime; source-only arm bounds overhead outside automatic mode.",
            "project_harness": "Package tests frozen at25; benchmark file frozen at33 in both binaries. Only unrelated physical-output regression is omitted to avoid APIs absent at25; no benchmark code changes.",
        },
        "samples": [], "complete": False,
    }
    manifest_path = output / "run-manifest.json"

    def save():
        manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")

    save()
    start = time.monotonic()
    for group in selected:
        for round_index in range(6):
            for label, _, _, _ in balanced(group["arms"], round_index):
                for run in [entry for entry in runs if entry["group"] == group["name"] and entry["arm"] == label]:
                    command = [run["binary"], "-test.run=^$", "-test.bench=" + run["pattern"], "-test.benchtime=" + run["benchtime"], "-test.benchmem", "-test.count=1", "-test.timeout=10m"]
                    began = time.monotonic()
                    result = subprocess.run(command, cwd=manifest["cwd"], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
                    with (output / run["file"]).open("a") as file:
                        file.write(result.stdout)
                    names = [re.sub(r"-\d+$", "", line.split()[0]) for line in result.stdout.splitlines() if line.startswith("Benchmark")]
                    valid = result.returncode == 0 and result.stdout.splitlines().count("PASS") == 1 and sorted(names) == run["expected_benchmarks"]
                    manifest["samples"].append({"group": group["name"], "arm": label, "workload": run["workload"], "round": round_index + 1, "returncode": result.returncode, "valid": valid, "seconds": time.monotonic() - began})
                    save()
                    if not valid:
                        raise SystemExit("Invalid sample in " + run["file"] + ":\n" + result.stdout)
            print(f"{group['name']}: round {round_index + 1}/6, {time.monotonic() - start:.1f}s elapsed", flush=True)
    manifest["complete"] = True
    save()
    print("All selected comparisons complete.", flush=True)


if __name__ == "__main__":
    main()
