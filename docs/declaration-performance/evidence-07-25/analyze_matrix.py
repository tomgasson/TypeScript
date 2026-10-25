#!/usr/bin/env python3
"""Summarize completed benchmark groups without running benchmark binaries.

Only groups with six PASS-terminated runs and every expected benchmark in every
run enter the comparisons. Exact two-sided Mann-Whitney p values enumerate all
924 label assignments for six versus six observations, retaining tied ranks.
These are exploratory, unpaired tests, without multiple-comparison correction.
"""

import argparse
from collections import defaultdict
from itertools import combinations
import json
import math
from pathlib import Path
import re
import statistics


HERE = Path(__file__).resolve().parent
DEFAULT_REPO = next((p for p in HERE.parents if (p / "tsc/go.mod").is_file()), Path.cwd())
ROOT = DEFAULT_REPO / "_declaration-perf/matrix"


def cases(prefix, middle, leaves):
    return {"/".join([prefix, case, leaf]) for case in middle for leaf in leaves}


ELIGIBILITY = ["JSON", "SkippedJS", "DeclarationFile", "EligibleTS", "ReferenceSource"]
DECLARATION = ["AnnotatedTS_1", "AnnotatedTS_200", "JSExpando", "JSCommonJS"]
WARM = ["WarmDiagnosticsUncached", "WarmEmit"]
EXPECTED = {
    ("eligibility", "requests"): cases("BenchmarkDeclarationEligibility", ELIGIBILITY, ["ColdProgramDiagnostics", "WarmDiagnostics"]),
    ("eligibility", "heap"): {"BenchmarkDeclarationEligibilityRetainedHeap/" + case for case in ELIGIBILITY},
    ("declaration", "work"): cases("BenchmarkDeclaration", DECLARATION, WARM) | {"BenchmarkDeclaration/AnnotatedTS_200/ColdProgramDiagnostics"},
    ("declaration", "heap"): {"BenchmarkDeclarationRetainedHeap"},
    ("classes", "work"): cases("BenchmarkDeclarationClasses", ["AnnotatedTS_1", "AnnotatedTS_200", "JSFullSignature_1", "JSFullSignature_200"], WARM),
    ("emitter-retention", "heap"): {"BenchmarkEmitCompletedWorkerRetention"},
    ("metadata", "work"): {"BenchmarkDeclaration/AnnotatedTS_1/WarmEmit"},
    ("checker-lease", "work"): {"BenchmarkDeclaration/AnnotatedTS_1/WarmEmit"},
    ("parameters", "work"): cases("BenchmarkDeclaration", ["AnnotatedTS_200", "JSCommonJS"], WARM),
    ("parser", "work"): {"BenchmarkParse/" + case for case in ["empty.ts", "checker.ts", "dom.generated.d.ts", "Herebyfile.mjs", "jsxComplexSignatureHasApplicabilityError.tsx"]},
    ("parser", "heap"): {"BenchmarkReturnedParserScannerRetention"},
    ("boundary", "work"): {"BenchmarkDeclarationBoundary/" + case for case in ["SourceConsumer", "ReusedDeclarations", "GenerateDeclarations", "GenerateAndCheck"]},
    ("boundary", "heap"): {"BenchmarkDeclarationBoundaryRetainedHeap/" + case for case in ["SourceConsumer", "Declarations"]},
}
CUSTOM_METRICS = {
    ("eligibility", "heap"): {"live-B/program"},
    ("declaration", "heap"): {"live-B/program"},
    ("emitter-retention", "heap"): {"live-B/paused-emit", "live-completed-checkers"},
    ("parser", "heap"): {"live-B/returned-parser"},
    ("boundary", "heap"): {"live-B/batch", "live-B/consumer", "shared-declaration-B"},
}


def exact_mwu(before, after):
    pooled = before + after
    ordered = sorted(range(len(pooled)), key=pooled.__getitem__)
    ranks2 = [0] * len(pooled)
    start = 0
    while start < len(ordered):
        end = start + 1
        while end < len(ordered) and pooled[ordered[end]] == pooled[ordered[start]]:
            end += 1
        for index in ordered[start:end]:
            ranks2[index] = start + end + 1  # twice the average 1-based rank
        start = end
    n, m = len(before), len(after)
    u2 = sum(ranks2[:n]) - n * (n + 1)
    distance = abs(u2 - n * m)
    extreme = sum(
        abs(sum(ranks2[index] for index in indices) - n * (n + 1) - n * m) >= distance
        for indices in combinations(range(n + m), n)
    )
    permutations = math.comb(n + m, n)
    return {"u_before": u2 / 2, "p_two_sided_exact": extreme / permutations, "extreme_assignments": extreme, "assignments": permutations}


def distribution(values):
    return {"n": len(values), "median": statistics.median(values), "min": min(values), "max": max(values), "samples": values}


def compare(before, after):
    result = {}
    for metric in sorted(before.keys() & after.keys()):
        old, new = before[metric], after[metric]
        difference = new["median"] - old["median"]
        result[metric] = {
            "before": old, "after": new, "delta": difference,
            "delta_percent": difference / old["median"] * 100 if old["median"] != 0 else None,
            **exact_mwu(old["samples"], new["samples"]),
        }
    return result


def read_run(root, entry, rounds):
    text = (root / entry["file"]).read_text()
    blocks, current, errors = [], {}, []
    for line in text.splitlines():
        if line.startswith("Benchmark"):
            columns = line.split()
            name = re.sub(r"-\d+$", "", columns[0])
            if name in current:
                errors.append("repeated benchmark before PASS: " + name)
            if len(columns) < 4 or (len(columns) - 2) % 2:
                errors.append("malformed benchmark: " + line)
                continue
            try:
                current[name] = {columns[i + 1]: float(columns[i]) for i in range(2, len(columns), 2)}
                if not all(math.isfinite(value) for value in current[name].values()):
                    errors.append("nonfinite benchmark metric: " + line)
            except ValueError:
                errors.append("nonnumeric benchmark: " + line)
        elif line == "PASS":
            blocks.append(current)
            current = {}
        elif line.startswith("FAIL") or line.startswith("panic:"):
            errors.append(line)
    expected = EXPECTED[entry["group"], entry["workload"]]
    required_metrics = {"ns/op", "B/op", "allocs/op"} | CUSTOM_METRICS.get((entry["group"], entry["workload"]), set())
    for index, block in enumerate(blocks):
        if set(block) != expected:
            errors.append(f"run {index + 1}: missing={sorted(expected - block.keys())}, unexpected={sorted(block.keys() - expected)}")
        for name, metrics in block.items():
            missing = required_metrics - metrics.keys()
            if missing:
                errors.append(f"run {index + 1}, {name}: missing metrics={sorted(missing)}")
    complete = len(blocks) == rounds and not current and not errors
    result = {**entry, "passes": len(blocks), "complete": complete, "errors": errors}
    if complete:
        samples = defaultdict(lambda: defaultdict(list))
        for block in blocks:
            for name, metrics in block.items():
                for metric, value in metrics.items():
                    samples[name][metric].append(value)
        if any(len(values) != rounds for metrics in samples.values() for values in metrics.values()):
            result.update(complete=False, errors=["metric was not present in every run"])
        else:
            result["benchmarks"] = {name: {metric: distribution(values) for metric, values in metrics.items()} for name, metrics in samples.items()}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--output", type=Path, help="default: <root>/summary.json; choose a temporary path to preserve archived evidence")
    args = parser.parse_args()
    root = args.root.resolve()
    manifest = json.loads((root / "run-manifest.json").read_text())
    runs = [read_run(root, entry, manifest["rounds"]) for entry in manifest["runs"]]
    groups = {}
    comparisons = []
    for group in dict.fromkeys(entry["group"] for entry in runs):
        entries = [entry for entry in runs if entry["group"] == group]
        complete = all(entry["complete"] for entry in entries)
        groups[group] = {"complete": complete, "runs": entries}
        if not complete:
            continue
        variants = list(dict.fromkeys(entry["variant"] for entry in entries))
        for before_variant, after_variant in combinations(variants, 2):
            for workload in dict.fromkeys(entry["workload"] for entry in entries):
                before = next(entry["benchmarks"] for entry in entries if entry["variant"] == before_variant and entry["workload"] == workload)
                after = next(entry["benchmarks"] for entry in entries if entry["variant"] == after_variant and entry["workload"] == workload)
                for name in sorted(before.keys() & after.keys()):
                    comparisons.append({"group": group, "workload": workload, "before_variant": before_variant, "after_variant": after_variant, "benchmark": name, "metrics": compare(before[name], after[name])})
    boundary = {}
    if groups.get("boundary", {}).get("complete"):
        work = next(entry["benchmarks"] for entry in groups["boundary"]["runs"] if entry["workload"] == "work")
        heap = next(entry["benchmarks"] for entry in groups["boundary"]["runs"] if entry["workload"] == "heap")
        prefix = "BenchmarkDeclarationBoundary/"
        source = work[prefix + "SourceConsumer"]
        reused = work[prefix + "ReusedDeclarations"]
        generation = work[prefix + "GenerateDeclarations"]
        boundary["source_vs_reused_declarations"] = compare(source, reused)
        boundary["source_vs_generate_and_check"] = compare(source, work[prefix + "GenerateAndCheck"])
        boundary["retained_heap"] = compare(heap["BenchmarkDeclarationBoundaryRetainedHeap/SourceConsumer"], heap["BenchmarkDeclarationBoundaryRetainedHeap/Declarations"])
        saving = source["ns/op"]["median"] - reused["ns/op"]["median"]
        if saving > 0:
            threshold = generation["ns/op"]["median"] / saving
            boundary["estimated_generation_break_even_checks"] = threshold
            boundary["estimated_first_integer_check_count_faster"] = math.floor(threshold) + 1
    summary = {
        "complete": all(group["complete"] for group in groups.values()),
        "method": "medians; exact two-sided Mann-Whitney label permutation including tied ranks; unpaired exploratory comparisons, no multiple-testing correction",
        "heap_caution": "Heap workload ns/op includes setup/batches or time to paused emit; use custom live-B metrics for retention, not these timings. Post-GC live heap is not RSS or peak memory.",
        "groups": groups, "comparisons": comparisons, "boundary": boundary,
    }
    (args.output or root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    for name, group in groups.items():
        progress = ", ".join(f"{entry['variant']}/{entry['workload']}={entry['passes']}" for entry in group["runs"])
        print(f"{name}: {'COMPLETE' if group['complete'] else 'pending'} ({progress})")
        for entry in group["runs"]:
            for error in entry["errors"]:
                print(f"  {entry['file']}: {error}")


if __name__ == "__main__":
    assert exact_mwu(list(range(6)), list(range(6, 12)))["p_two_sided_exact"] == 2 / 924
    assert exact_mwu([1] * 6, [1] * 6)["p_two_sided_exact"] == 1
    main()
