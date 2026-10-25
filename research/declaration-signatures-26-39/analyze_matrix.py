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


ROOT = Path(__file__).resolve().parent / "matrix"


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
    expected = set(entry["expected_benchmarks"])
    required_metrics = {"ns/op", "B/op", "allocs/op"}
    if entry["package"] == "project":
        required_metrics |= ({"live-B/batch", "live-B/session", "shared-declaration-B", "cache-entries/session", "declaration-text-B/session"}
            if entry["workload"] == "heap" else {"producer-source-reads/op", "producer-source-read-B/op"})
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


def normalize_name(name, mode):
    if mode is None:
        return name
    suffix = "/" + mode
    if not name.endswith(suffix):
        raise ValueError("Benchmark mode does not match its manifest: " + name)
    return name[:-len(suffix)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    args = parser.parse_args()
    root = args.root.resolve()
    manifest = json.loads((root / "run-manifest.json").read_text())
    runs = [read_run(root, entry, manifest["rounds"]) for entry in manifest["runs"]]
    groups, comparisons = {}, []
    for group in dict.fromkeys(entry["group"] for entry in runs):
        entries = [entry for entry in runs if entry["group"] == group]
        sample_errors = []
        for entry in entries:
            samples = [sample for sample in manifest["samples"] if all(sample[key] == entry[key] for key in ["group", "arm", "workload"])]
            if len(samples) != manifest["rounds"] or {sample["round"] for sample in samples} != set(range(1, manifest["rounds"] + 1)):
                sample_errors.append(entry["file"] + ": runner sample rounds incomplete")
            elif any(not sample["valid"] or sample["returncode"] != 0 for sample in samples):
                sample_errors.append(entry["file"] + ": runner recorded invalid sample")
        complete = all(entry["complete"] for entry in entries) and not sample_errors
        groups[group] = {"complete": complete, "runs": entries, "runner_errors": sample_errors}
        if not complete:
            continue
        arms = list(dict.fromkeys(entry["arm"] for entry in entries))
        for before_arm, after_arm in combinations(arms, 2):
            for workload in dict.fromkeys(entry["workload"] for entry in entries):
                before_entry = next(entry for entry in entries if entry["arm"] == before_arm and entry["workload"] == workload)
                after_entry = next(entry for entry in entries if entry["arm"] == after_arm and entry["workload"] == workload)
                before = {normalize_name(name, before_entry["mode"]): metrics for name, metrics in before_entry["benchmarks"].items()}
                after = {normalize_name(name, after_entry["mode"]): metrics for name, metrics in after_entry["benchmarks"].items()}
                if before.keys() != after.keys():
                    raise ValueError("Benchmark names differ between arms: " + group)
                for name in sorted(before):
                    if before[name].keys() != after[name].keys():
                        raise ValueError("Metrics differ between arms: " + group + "/" + name)
                    comparisons.append({"group": group, "workload": workload, "before_arm": before_arm, "after_arm": after_arm,
                        "before_variant": before_entry["variant"], "after_variant": after_entry["variant"], "benchmark": name,
                        "metrics": compare(before[name], after[name])})
    summary = {
        "complete": manifest["complete"] and all(group["complete"] for group in groups.values()),
        "method": "Six process samples per arm; medians; exact two-sided Mann-Whitney label permutation including tied ranks; unpaired exploratory comparisons, no multiple-testing correction. P values test rank distributions, not confidence in a median difference or equivalence.",
        "heap_caution": "Heap ns/op and allocations include complete batches of four Sessions and 16 edits each; use custom live-B/session for retention. Post-GC marginal live heap is not RSS or peak memory. Shared fixture predates baseline; automatic outputs are generated separately per Session.",
        "read_caution": "Producer source reads/bytes count successful underlying FS ReadFile operations; snapshotFS cached inputs can satisfy complete regeneration without additional underlying reads. These are not extraction counts or cache hit/miss counts.",
        "source_manifest": str(root / "run-manifest.json"), "groups": groups, "comparisons": comparisons,
    }
    (root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    findings = ["# Benchmark findings", "", summary["method"], "", summary["heap_caution"], "", summary["read_caution"], ""]
    for name, group in groups.items():
        progress = ", ".join(f"{entry['arm']}/{entry['workload']}={entry['passes']}" for entry in group["runs"])
        print(f"{name}: {'COMPLETE' if group['complete'] else 'pending'} ({progress})")
        for entry in group["runs"]:
            for error in entry["errors"]:
                print(f"  {entry['file']}: {error}")
        for error in group["runner_errors"]:
            print("  " + error)
        if not group["complete"]:
            continue
        findings += ["## " + name, "", "| Case / arms | Metric | Before median | After median | Change | Exact p |", "| --- | --- | ---: | ---: | ---: | ---: |"]
        for entry in comparisons:
            if entry["group"] != name:
                continue
            if entry["workload"] == "heap":
                metrics = ["live-B/session", "cache-entries/session", "declaration-text-B/session", "shared-declaration-B"]
            else:
                metrics = ["ns/op", "B/op", "allocs/op", "producer-source-reads/op", "producer-source-read-B/op"]
            for metric in metrics:
                if metric not in entry["metrics"]:
                    continue
                result = entry["metrics"][metric]
                pct = result["delta_percent"]
                change = f"{pct:+.2f}%" if pct is not None else f"{result['delta']:+g}"
                label = entry["benchmark"] + ": " + entry["before_arm"] + " → " + entry["after_arm"]
                findings.append(f"| {label} | {metric} | {result['before']['median']:g} | {result['after']['median']:g} | {change} | {result['p_two_sided_exact']:.5f} |")
        findings.append("")
    (root / "findings.md").write_text("\n".join(findings))


if __name__ == "__main__":
    assert exact_mwu(list(range(6)), list(range(6, 12)))["p_two_sided_exact"] == 2 / 924
    assert exact_mwu([1] * 6, [1] * 6)["p_two_sided_exact"] == 1
    main()
