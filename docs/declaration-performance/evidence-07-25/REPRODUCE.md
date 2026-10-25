This bundle contains the recorded benchmark samples, build/run provenance, analysis and validation logs for steps 07–25. It contains no benchmark binaries, generated Go overlays, or copied production sources. The scripts regenerate those temporary build inputs from Git.

`published-07-25.json` maps each original local build/harness revision to its published `tomgasson/TypeScript` commit by equality of the **complete Git tree**, with step 06 included as the cumulative comparator. The original raw manifests retain their local paths and commit IDs. Source-tree equality permits reproduction from the public commits even though author/timestamp/parent metadata changed during publication. Rebuilt binary hashes need not match across paths, toolchains or build metadata; recorded hashes identify the actual binaries measured.

| Evidence | Contents |
| --- | --- |
| `matrix/` | Raw output, six-round run manifest and complete `summary.json`. |
| `editor/` | Four raw output files and the separate six-round editor run manifest. |
| `variants/` | Both original compiler build manifests. `build-variants-06-10-11.json` records completed builds for 06/10/11 and prepared later variants; `build-variants.json` records completed builds for 12/13/14/15/19/20. |
| `variants-22-24/`, `variants-parser/` | Complete additional compiler and parser build manifests. |
| `validation/` | Original full/relevant-package runs, corrected step-23/24 check-order reruns, passing final compiler/project run, initial race build disk failure and passing retry. |
| `initial07/` | Superseded exploratory before/after samples and summary; these are not the final cumulative comparison. |
| `fixture-inputs.json` | Hashes of tracked parser runtime fixtures and dependency lockfiles. |

Raw build manifests contain only the explicitly selected Go build environment fields: target/platform, CGO, GOPATH/GOCACHE, GOTOOLCHAIN, GOWORK and GOFLAGS. They are not complete environment dumps. Inspection found no credentials or credential-bearing URLs, so no evidence redaction was required.

Use Python 3.10 or later, Git, Go **1.27.3** (the measured compiler), and a Node version satisfying the root package's engine constraint. The original Node diagnostic log records 24.22.1; the repository pins its npm version in `package.json`. Install locked npm dependencies for the JS comparison tests, and download Go modules before starting timed work. The parser throughput fixtures are tracked files under `tsc/testdata/fixtures`; they do not require npm. Run measurements on an otherwise quiet machine with enough disk for several Go test binaries and build caches.

The commands below assume a checkout of the published stack with this evidence directory available. Fetch only the user's fork, then verify the branch trees against `published-07-25.json`. The scripts accept either local `refactor/...` branches or `origin/refactor/...` remote-tracking refs. For an exact historical reproduction, the published commit IDs in the provenance map are authoritative if a branch later moves.

```bash
git fetch https://github.com/tomgasson/TypeScript.git '+refs/heads/refactor/*:refs/remotes/origin/refactor/*'
go version
node --version
npm --version
npm ci --ignore-scripts
GOTOOLCHAIN=local go -C tsc mod download

PERF_REPO="$PWD"
PERF_EVIDENCE="$PERF_REPO/docs/declaration-performance/evidence-07-25"
PERF_WORK="$PERF_REPO/_declaration-perf"
mkdir -p "$PERF_WORK"
```

Run this read-only provenance check before building. It also verifies the external parser fixtures and lockfiles. A checkout later than step 25 containing only evidence/documentation additions has the same relevant production inputs; the overlay builder rejects non-Go differences under `tsc`.

```bash
python - "$PERF_REPO" "$PERF_EVIDENCE" <<'PY'
import hashlib, json, subprocess, sys
from pathlib import Path
repo, evidence = map(Path, sys.argv[1:])
for item in json.loads((evidence / "published-07-25.json").read_text())["revisions"]:
    actual = subprocess.check_output([
        "git", "-C", str(repo), "rev-parse", item["published_commit"] + "^{tree}"
    ], text=True).strip()
    assert actual == item["tree"], item["branch"]
    candidates = [item["branch"], "refs/remotes/origin/" + item["branch"]]
    for ref in candidates:
        result = subprocess.run(["git", "-C", str(repo), "rev-parse", "--verify", ref + "^{tree}"], capture_output=True, text=True)
        if result.returncode == 0:
            assert result.stdout.strip() == item["tree"], ref
            break
    else:
        raise RuntimeError("Missing branch: " + item["branch"])
for item in json.loads((evidence / "fixture-inputs.json").read_text())["inputs"]:
    assert hashlib.sha256((repo / item["file"]).read_bytes()).hexdigest() == item["sha256"], item["file"]
print("Published trees, available variant refs and fixture hashes match.")
PY
```

Build serially, with a common test harness within each comparison. Each variant uses the named production revision plus tests from the specified harness revision. This is deliberately a benchmark binary assembled with overlays, not an untouched checkout of its production commit. Other package tests are excluded. `declaration_bench_test.go` uses the matching production revision because the private checker-acquisition helper changed in step 20; this affects setup before timing, preserving the measured operation. The shared tests in the step-20 and step-24 harnesses have identical source hashes where they overlap.

```bash
python "$PERF_EVIDENCE/build_variants.py" 06 10 11 12 13 14 15 19 20 \
  --repo "$PERF_REPO" --output-dir "$PERF_WORK/variants" \
  --test-ref refactor/20-explicit-emit-checker-lease --build

python "$PERF_EVIDENCE/build_variants.py" 22 23 24 \
  --repo "$PERF_REPO" --output-dir "$PERF_WORK/variants-22-24" \
  --test-ref refactor/24-skip-single-declaration-overload-probes --build

python "$PERF_EVIDENCE/build_variants.py" 20 21 --package parser --no-trimpath \
  --repo "$PERF_REPO" --output-dir "$PERF_WORK/variants-parser" \
  --test-ref refactor/21-release-pooled-scanner-inputs --build
```

`--go /absolute/path/to/go` selects a toolchain outside PATH; `--gopath` is optional. The builder uses `GOTOOLCHAIN=local`, `GOWORK=off`, empty GOFLAGS, `-tags=embed`, `-buildvcs=false`, and build parallelism of two. Compiler variants retain `-trimpath`. Both parser variants must omit it because fixture discovery uses the compiler-recorded repository path. Keep the source checkout available while running those binaries. Do not run new regression tests against the old-production/common-harness overlay binaries; their intended entry point is `-test.run=^$`.

```bash
python "$PERF_EVIDENCE/run_matrix.py" --repo "$PERF_REPO" --workspace "$PERF_WORK"
python "$PERF_EVIDENCE/analyze_matrix.py" --root "$PERF_WORK/matrix"
```

The runner preserves the original patterns and durations: six rounds, `GOMAXPROCS=1`, `GOGC=100`, one-second work samples, 500 ms parser throughput samples, and one-iteration retention samples. Variant order alternates for pairs and rotates/reverses for the three class variants. Each sample runs in a fresh process. `--groups` selects a subset; use a different output directory for each subset. Existing result files or manifests require an explicit `--overwrite`. The scripts' defaults write under `_declaration-perf`, not into this archived evidence.

Analyze the archived matrix without rerunning benchmarks or replacing its summary:

```bash
python "$PERF_EVIDENCE/analyze_matrix.py" --root "$PERF_EVIDENCE/matrix" \
  --output "$PERF_WORK/archived-summary.json"
```

The analyzer rejects incomplete PASS-terminated runs, missing benchmarks and missing metrics. It reports medians and exact two-sided Mann–Whitney label-permutation p-values with tied ranks. Comparisons are exploratory and unpaired, without multiple-testing correction. Six versus six samples provide 924 assignments. Heap-workload timings include setup or batch operations; use their custom live-byte metrics for retention, not their `ns/op` values.

Build the editor test binary from step 25's production and project-test tree, or the evidence branch with no production changes since step 25. Unlike the compiler variants, this is a normal project-package build. Its run manifest labels the asserted runtime source tree and records the binary hash; the runner does not infer source provenance from a binary.

```bash
GOTOOLCHAIN=local GOWORK=off GOFLAGS= GOMAXPROCS=2 go -C tsc test -c \
  -tags=embed -p=2 -buildvcs=false -o "$PERF_WORK/project25.test" ./internal/project
python "$PERF_EVIDENCE/run_editor.py" --repo "$PERF_REPO" --workspace "$PERF_WORK" \
  --runtime-ref refactor/25-editor-declaration-boundary-benchmarks
```

Editor runs alternate source/declaration arms in separate processes for six rounds. Work samples include a consumer edit, snapshot update, pull diagnostics and queue draining. Declarations are prepared before timing; producer edits, extraction and transport are excluded. Heap samples keep four Sessions after 16 edits each, include generated output once, and measure post-GC marginal heap before idle checker eviction. They are distinct from the fresh-Program compiler boundary benchmark. The four raw files retain all per-round values; the main report describes their medians and interpretation.

Validation can be rerun on the unmodified tip source tree with ordinary package tests, independently from benchmark overlays:

```bash
GOTOOLCHAIN=local go -C tsc test -tags=embed ./internal/compiler ./internal/project
GOTOOLCHAIN=local go -C tsc test -tags=embed ./internal/parser ./internal/checker \
  ./internal/execute/build ./internal/execute/incremental ./internal/execute/tsctests \
  ./internal/transpile ./internal/api ./internal/testrunner ./internal/fourslash/tests
GOTOOLCHAIN=local go -C tsc test -race -tags=embed -count=10 \
  -run 'TestEmit(ReleasesCompletedChecker|MetadataDoesNotRetainSourceFile)|TestCompilerHostGetSourceFileFromText|TestParserPoolReleasesScannerSource|TestDiagnosticSignature|TestDeclarationEmitAndSemanticCheckOrder|TestEditorDeclarationBoundary|Test.*Supplied.*' \
  ./internal/compiler ./internal/project ./internal/parser ./internal/execute/incremental
```

The saved broad run has three environment failures: missing npm TypeScript for `astnav`, unsupported filesystem `fanotify-no-rename`, and denied Unix socket creation for `ipc`. The missing dependency was subsequently installed. The saved relevant-package run also records an incorrect new fixture expectation, later corrected and verified against both steps 23 and 24; it remains a failed historical log. Final compiler/project tests pass. The first race attempt exhausted disk during parser/incremental builds after compiler/project passed; the saved retry passes the selected tests in all four packages with `-count=10`. None of these unsuccessful logs has been replaced with a successful one.

Source-tree and recorded-binary provenance were independently cross-checked: all 25 matrix run entries match completed build manifests, and all 14 distinct matrix binaries matched their recorded hashes during preparation. Step-13 and step-14 compiler binary hashes are equal because step 14 changes the incremental package outside that benchmark binary. The scripts were checked for syntax/argument handling and the archived matrix was reanalyzed; the portable copies have not been used to run another full benchmark/build matrix.
