# Declaration refactoring: measurements and limits

These changes improve the existing declaration path. They do not introduce signature
loading, change diagnostics, or release implementation ASTs from live Programs.

## Changes under comparison

| Label | Branch | Change |
| --- | --- | --- |
| before | `refactor/04-declaration-benchmarks` | Measurement harness; same production code as step 03 |
| maps | `refactor/05-lazy-expando-state` | Allocate the three expando maps only on their write paths |
| scan | `refactor/06-skip-inactive-commonjs-scan` | Also skip the CommonJS export-assignment walk when the binder's indicator is absent |

Step 05 resets the maps per source file, as before. Reads, iteration and deletion
accept nil maps. Initialization precedes recursive writes and preserves maps
populated by nested transformation. The ordinary late-statement map remains eager.
This saves 144 bytes and three allocations per annotated TypeScript transform;
the JavaScript controls save 48 bytes and one allocation. It is a small allocation
improvement, with no demonstrated throughput or retained-heap improvement alone.

Step 06 moves an existing condition ahead of a whole-tree traversal. The visitor's
only transformation is already guarded by `CommonJSModuleIndicator != nil`; without
it, the walk only visits children and temporarily sets/restores diagnostic context.
The inner guard stays. The separate expression/expando walk stays unconditional:
TypeScript function properties still require it. CommonJS files still execute both
walks. These invariants hold independently of `isolatedDeclarations`, so these
particular optimizations do not require that option to be enabled.

## Method

Measured on Linux/amd64, AMD EPYC 9V74, Go 1.27.3, `GOMAXPROCS=1`, `GOGC=100`.
Each variant was compiled once with the identical benchmark source. The broad run
used six samples of 300 ms per benchmark; a follow-up used six 1-second samples
for the large TypeScript fixture and the CommonJS controls. Variant order rotated
and reversed across rounds. No builds or other benchmarks ran concurrently.
This is a shared virtualized machine, not a dedicated performance host.

The TypeScript fixtures contain 1 or 200 explicitly annotated function/interface
pairs, with `isolatedDeclarations` enabled. The JavaScript controls cover an ES
module expando and a genuine `module.exports` assignment, without that option.
There is no import graph. Warm diagnostics deliberately bypass the Program's
diagnostic cache while retaining parsed source and a warm checker. Warm emit runs
the normal declaration emitter with a discard write callback: printing is included,
filesystem writes are excluded. Cold diagnostics include a fresh Program, standard
library parsing and checker initialization. They do not perform a full semantic
check of every implementation.

## Results

Medians; times below use the longer follow-up where available. Allocation counts
are from the broad run. `B/op` means allocated bytes, not live heap.

| Workload | Before | Maps only | Both changes | Both vs before |
| --- | ---: | ---: | ---: | ---: |
| TS 200: warm diagnostic transform | 1.749 ms | 1.730 ms | 1.203 ms | -31.2% |
| TS 200: warm declaration emit | 2.394 ms | 2.401 ms | 1.905 ms | -20.4% |
| TS 200: diagnostic allocated bytes | 943,872 | 943,728 | 589,872 | -37.5% |
| TS 200: diagnostic allocations | 18,508 | 18,505 | 10,701 | -42.2% |
| TS 200: emit allocated bytes | 1,125,962.5 | 1,125,818.5 | 771,954.5 | -31.4% |
| TS 200: emit allocations | 19,591 | 19,588 | 11,784 | -39.8% |
| TS 200: cold Program + declaration diagnostics | 41.400 ms | 41.176 ms | 40.800 ms | -1.4%; no demonstrated speedup |
| CommonJS: warm diagnostics | 24.403 us | 24.373 us | 24.771 us | +1.5%; inconclusive |
| CommonJS: warm emit | 36.464 us | 36.734 us | 36.190 us | -0.8%; inconclusive |

The initial short runs suggested a small maps-only slowdown and a CommonJS emit
slowdown. Longer follow-up did not reproduce a distinguishable difference. A
two-sided exact Mann-Whitney comparison of the longer samples gives p=0.0022 for
each large TS speedup, and p=0.59-1.00 (rounded) for the small maps-only/CommonJS differences.
These are exploratory comparisons without multiple-comparison correction;
non-significance does not establish equivalence or a universal no-regression claim.
Raw samples are included alongside this file, including the initial noisy results.

## Retained memory

The separate heap benchmark retains 16 fresh Programs after their declaration
diagnostics, with two GCs before and after. It reports the process heap delta divided
by 16. Shared fixture text, filesystem and configuration exist in both snapshots;
the increment includes per-Program parsed libraries, source ASTs, checker state and
diagnostic caches. Programs are kept alive explicitly through the second snapshot.

Median retained bytes per Program: **15,765,543 before; 15,765,577 maps;
15,765,561 both**. This is effectively unchanged. It is neither RSS nor peak heap.
The results demonstrate lower transient allocation; reduced GC pressure is plausible
but was not measured separately. They do not demonstrate implementation eviction.
No full LSP latency or large-project memory claim is
supported by these fixtures.

## Validation

- Compiler, transpile and incremental unit suites passed.
- Expando and transpile baselines passed after step 05 independently.
- Declaration, isolated-declaration, expando, CommonJS, content-mapper and JSON
  compiler baselines passed after step 06; accepted outputs were unchanged.
- Selected project-build tests for declaration-only emit, dependency updates and
  content-mapper incremental emit passed.
- No new public API, compiler option, cache policy or diagnostic format.

## Reproduction

With each branch checked out, build the same benchmark into a distinct binary:

```sh
go -C tsc test -tags=embed -c -o /tmp/declarations-before.test ./internal/compiler
# Repeat on the maps and scan branches with distinct output names.
```

From `tsc/internal/compiler`, run each binary in rotated/reversed order for six
rounds, one process at a time (replace `before` with the variant):

```sh
GOMAXPROCS=1 GOGC=100 /tmp/declarations-before.test -test.run='^$' \
  -test.cpu=1 -test.benchmem -test.bench='^BenchmarkDeclaration$' \
  -test.benchtime=300ms

GOMAXPROCS=1 GOGC=100 /tmp/declarations-before.test -test.run='^$' \
  -test.cpu=1 -test.benchmem -test.benchtime=1s \
  -test.bench='^BenchmarkDeclaration$/(AnnotatedTS_200|JSCommonJS)/(WarmDiagnosticsUncached|WarmEmit)$'

GOMAXPROCS=1 GOGC=100 /tmp/declarations-before.test -test.run='^$' \
  -test.cpu=1 -test.benchmem -test.bench='^BenchmarkDeclarationRetainedHeap$' \
  -test.benchtime=1x
```

For the heap benchmark, ordinary ns/op and B/op describe the entire 16-Program
batch; only `live-B/program` is normalized per Program.

## Where this leaves the architecture

We have removed an unnecessary implementation traversal from an existing
declaration path and reduced its temporary allocations. The path still constructs
a full source AST, acquires a checker through `newEmitHost`, and prints declarations
when emitting. The cold and retained-memory measurements expose those remaining
costs rather than hiding them behind a warm microbenchmark.

The next contribution sequence should isolate the existing per-file declaration
transformation from emit-host/checker acquisition and output policy. Preserve the
diagnostics-only versus supplemental-reference distinction. Then add internal
project-reference extraction gated by the producing project's resolved
`isolatedDeclarations` option, preserving errors for invalid files and mapping
diagnostics back to original source. Replacing disk reads with in-memory declaration
text would remove external codegen orchestration, but would still print/reparse.

Full implementation reclamation additionally requires removing the live Program,
checker, symbol-declaration and parse-cache references to those implementations.
An unprinted transformed AST is not yet an owned signature: it and its emit-context
original-node tables can still retain implementation nodes. That ownership change
needs its own tests and post-GC measurements before claiming a memory benefit.
