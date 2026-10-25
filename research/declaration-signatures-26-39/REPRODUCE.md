These scripts keep production revisions, benchmark fixtures and measurement output separate. Binaries are local build products and must not be committed.

The measured production revisions are:

| Comparison | Before | After | Shared benchmark fixture |
| --- | --- | --- | --- |
| Single-file transpilation | `19e34233bfe1aadb262d4a4b61b5dd51207a7c8f` (25) | `cda2c80535c31e0824392816a23f952ad334e545` (26) | 26 |
| Declaration modifier cleanup | `73dfc32cb10de002dba43d76655259ca64315ff4` (27) | `5aac8cbaccd3cbcb895a243ae7f0ba75e8dc4723` (28) | 28 |
| Ordinary source-route overhead | 25 | `52121ea757d9259238d5362ddf3188f82cb80297` (35) | `6496147f56a566562a1b1cc2f735d60276ae9d48` (33) |
| Source/prepared/automatic declarations and producer edits | 35 | Same runtime, different modes | 33 |

The project binaries use the package's existing tests from 25 and the same benchmark file from 33. The unrelated `TestEditorDeclarationBoundaryPhysicalOutputLifecycle` function is omitted from both benchmark overlays because it refers to APIs absent at 25. No benchmark function is changed. Original and transformed test hashes, omitted names, source/test commit and tree IDs, overlay hashes, build flags and binary hashes are recorded in each build manifest. The ordinary final-production test suite is separate evidence; these mixed-version binaries should only run with `-test.run=^$`.

Build serially with Go 1.27.3. The defaults match this workspace; when reproducing elsewhere, pass `--repo`, `--go` and `--gopath` explicitly to `build_all.py` and `--repo` to `run_matrix.py`.

```sh
python build_all.py --mode adjacent --build
python build_all.py --mode project \
  --final-ref 52121ea757d9259238d5362ddf3188f82cb80297 \
  --project-test-ref 6496147f56a566562a1b1cc2f735d60276ae9d48 --build
```

`build_all.py` resolves review branches to immutable commits before invoking `build_variants.py`. Production overlays include every changed Go source file since 25, every selected revision difference, and every later/dirty production change in the working tree. Files absent at an old revision are removed through the overlay. Extra package tests are excluded. A newly changed, previously uncovered production file aborts a build rather than leaking into the comparison.

Compiler and transpile pairs consistently use `-trimpath`; both project binaries omit it. Every build uses `-tags=embed -p=2 -buildvcs=false`, the pinned local toolchain and `GOWORK=off`. Compare only the matching package/variant pairs.

After **all builds and tests have stopped**, inspect the run plan and explicitly start it:

```sh
python run_matrix.py
python run_matrix.py --run
```

The runner verifies binary hashes before execution. It runs six balanced rounds in separate processes, with `GOMAXPROCS=1`, `GOGC=100`, one-second work samples and separate `-benchtime=1x` heap samples. Pair order alternates; the three consumer modes rotate/reverse across six rounds. Every sample must exit successfully, contain one `PASS`, and contain exactly its expected benchmark cases. Raw stdout and per-sample status/timing are retained. An existing run manifest or nonempty result is protected unless `--replace` is passed; use another `--output-dir` for independent reruns.

The editor fixture retains its original scale: 48 producer modules with 256 body locals each, plus a barrel. Four Sessions and 16 edits per Session are used for retained-heap samples. Producer scenarios cover a body-only edit, a public return type, `declarationDir`, and a transitive leaf type whose forwarding declaration text stays unchanged. Producer edit timing includes the file write, watched-file update, regeneration when needed, consumer diagnostics and background queue draining.

Automatic output has a per-loader singleflight cache. Each full consumer rebuild regenerates the complete producer; there is no persistent extraction cache between snapshots. Consumer-only edits use normal Program cloning and existing declaration ASTs/output. The source-only 25-versus-35 comparison bounds overhead outside automatic mode; source/prepared/automatic comparisons use the same final runtime.

`producer-source-reads/op` and `producer-source-read-B/op` count successful underlying filesystem reads only. SnapshotFS may serve cached source inputs even when the producer regenerates, so these metrics are **not extraction-call counts or cache hit/miss counts**. Tree identity and invalidation correctness are asserted separately by tests.

`live-B/session` is post-GC marginal heap, not peak heap or RSS. The common source fixture exists before the baseline. Prepared declaration text is generated once and shared; automatic output is generated independently by each Session after the baseline. `shared-declaration-B` describes the prepared output only. `declaration-text-B/session` is logical declaration text size; it does not describe whether backing storage is shared. Actual retained storage and tracking state are included in the heap delta. Timing fields from the heap batches are not editor latency measurements.

After collection, run `python analyze_matrix.py`. It requires six complete PASS blocks, exact expected cases, finite metrics and matching per-round runner success records before including a group. It writes `matrix/summary.json` and `matrix/findings.md`. Comparisons use medians and an exact two-sided Mann–Whitney label-permutation test with tied ranks (all 924 assignments for six versus six samples). These are exploratory, unpaired tests without multiple-comparison correction; they do not provide confidence intervals for median changes or demonstrate equivalence when a difference is not detected.

The recorded run finished in 333.2 seconds: all 156 subprocess samples passed. Production/test identities and binary hashes are captured in `matrix/run-manifest.json`; no builds or tests overlapped the timed run.
