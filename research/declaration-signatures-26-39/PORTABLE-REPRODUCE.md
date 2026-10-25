# Reproducing the measured matrix from the published fork

This bundle preserves the original measurement records. Work in a **new directory** when rebuilding or collecting another run; the archived manifests contain original absolute paths and local commit IDs. `PROVENANCE.json` maps those IDs to published commits with the same full Git trees. The commands below use immutable published IDs and bypass `build_all.py`'s workspace-specific local branch assumptions.

Prerequisites: Linux, Python 3, Git available at `/usr/bin/git`, Go **1.27.3**, and a dedicated checkout of `tomgasson/TypeScript` containing the published commits below. Use the complete checkout, including tracked `tsc/internal/bundled/libs` and test inputs; Go module dependencies must be available through the normal Go module cache/download mechanism. The measured machine was linux/amd64, AMD EPYC 9V74; timings and fresh binary hashes need not match on a different machine or path.

Set these paths to your environment. `TS_REPRO_RUN` must be a fresh directory outside the evidence archive. `TS_REPRO_REPO` must be a clean, dedicated checkout, because the next command selects the measured source tree.

```sh
TS_EVIDENCE=/absolute/path/to/evidence
TS_REPRO_RUN=/absolute/path/to/new-run
TS_REPRO_REPO=/absolute/path/to/TypeScript-checkout
TS_REPRO_GO=/absolute/path/to/go1.27.3/bin/go
TS_REPRO_GOPATH=/absolute/path/to/go-workspace

TS_BASE25=19e34233bfe1aadb262d4a4b61b5dd51207a7c8f
TS_AFTER26=87f275782fb54d29475aeb3ddd7adc38b9bba587
TS_BEFORE27=7fb6e44af580ffb29c621bdf3dac3da090098bf9
TS_AFTER28=6f6a82e68e020667056606a2859ac2b2e5228410
TS_HARNESS33=135d0c1b8a6785b8dd2a571b59f5c40541a4adf2
TS_FINAL35=72e8095fa3bd757d724afaafe7d234b740dc6e05

git -C "$TS_REPRO_REPO" checkout --detach "$TS_FINAL35"
mkdir -p "$TS_REPRO_RUN"
cp "$TS_EVIDENCE/build_variants.py" "$TS_EVIDENCE/run_matrix.py" "$TS_EVIDENCE/analyze_matrix.py" "$TS_REPRO_RUN/"
"$TS_REPRO_GO" version
```

Build each pair serially. The CLI takes positional `LABEL=REF` arguments and requires `--package` and `--test-ref`. `--build` is what actually compiles; without it the builder only prepares source overlays/manifests. Every command supplies the same explicit baseline and local paths.

```sh
python "$TS_REPRO_RUN/build_variants.py" \
  --repo "$TS_REPRO_REPO" --go "$TS_REPRO_GO" --gopath "$TS_REPRO_GOPATH" \
  --base-ref "$TS_BASE25" --package transpile --test-ref "$TS_AFTER26" \
  --output-dir "$TS_REPRO_RUN/variants-transpile" \
  "25=$TS_BASE25" "26=$TS_AFTER26" --build

python "$TS_REPRO_RUN/build_variants.py" \
  --repo "$TS_REPRO_REPO" --go "$TS_REPRO_GO" --gopath "$TS_REPRO_GOPATH" \
  --base-ref "$TS_BASE25" --package compiler --test-ref "$TS_AFTER28" \
  --output-dir "$TS_REPRO_RUN/variants-compiler" \
  "27=$TS_BEFORE27" "28=$TS_AFTER28" --build

python "$TS_REPRO_RUN/build_variants.py" \
  --repo "$TS_REPRO_REPO" --go "$TS_REPRO_GO" --gopath "$TS_REPRO_GOPATH" \
  --base-ref "$TS_BASE25" --package project --test-ref "$TS_BASE25" \
  --test-override "tsc/internal/project/declaration_boundary_bench_test.go=$TS_HARNESS33" \
  --omit-test-function TestEditorDeclarationBoundaryPhysicalOutputLifecycle \
  --no-trimpath --output-dir "$TS_REPRO_RUN/variants-project" \
  "25=$TS_BASE25" "final=$TS_FINAL35" --build
```

The two compiler/transpile pairs both use `-trimpath`; both project binaries omit it to preserve repository-root discovery. All builds use `-tags=embed -p=2 -buildvcs=false`, `GOTOOLCHAIN=local`, `GOWORK=off`, empty `GOFLAGS`, and build-time `GOMAXPROCS=2`. The project omission removes only an unrelated regression test requiring APIs unavailable at25; it is applied identically to both variants. Benchmark functions are unchanged. These mixed-harness binaries are for benchmark execution with `-test.run=^$`, not the final correctness suite.

The builder overlays every changed production Go file and freezes the selected package's tests, excluding later extra tests. It rejects changed non-Go inputs rather than treating them as frozen. Build manifests record source trees, per-file hashes, original/transformed test hashes, exclusions, flags and new binary hashes. Use a complete checkout at the selected runtime to keep non-Go embedded inputs consistent.

Once **all builds and correctness tests have stopped**, inspect the plan, then collect the matrix. The runner reads the three `variants-*` directories beside its script and verifies the freshly built binary hashes.

```sh
python "$TS_REPRO_RUN/run_matrix.py"
python "$TS_REPRO_RUN/run_matrix.py" --repo "$TS_REPRO_REPO" --run
python "$TS_REPRO_RUN/analyze_matrix.py" --root "$TS_REPRO_RUN/matrix"
```

The default plan performs no measurements. `--run` executes six balanced rounds with `GOMAXPROCS=1`, `GOGC=100`, one-second work samples and separate `1x` heap samples. It passes `-test.run=^$ -test.benchmem -test.count=1 -test.timeout=10m`, captures full stdout, validates exact benchmark cases and successful exit/PASS, and records each round. Pair order alternates; three consumer modes rotate/reverse. The reference run took333.2 seconds after builds.

Use `--group NAME` for a subset and `--output-dir /new/path` for a separate result directory; run the analyzer with matching `--root`. The runner refuses an existing run manifest or selected nonempty output unless `--replace` is passed. **Do not use `--replace` on the evidence archive.** It starts a new manifest for the selected groups; it does not merge a prior run. Binaries must still live in the script-adjacent `variants-*` directories.

`build_all.py` is also included as the original convenience wrapper. It accepts `--mode adjacent|project|all`, `--build`, `--repo`, `--go`, `--gopath`; project/all also require `--final-ref` and `--project-test-ref`. It resolves local `refactor/25-*` through `refactor/28-*` branches, so use the explicit immutable commands above when those local refs do not exist. Original `build-plan-*.json` files describe the actual local run and should remain unchanged.

See `REPRODUCE.md` for fixture/method limits, `PROVENANCE.md` for local/published identity, and `matrix/summary.json` for all samples and exact exploratory statistics. The original measured manifests retain local IDs; newly built manifests should accurately record the published IDs actually used. No new benchmarking was performed while packaging this evidence.
