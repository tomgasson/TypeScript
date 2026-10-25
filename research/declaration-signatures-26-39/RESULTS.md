# Automatic project-reference declarations: steps 26–39

Code branches are published only in `tomgasson/TypeScript`. No upstream pull requests, issues, or comments were created. Each branch adds one step to the previous branch. Measurements use the exact production trees recorded below; later correctness changes are separately identified.

The new feature supplies missing project-reference declarations directly to the consuming compiler and language service. It uses the producer's ordinary declaration emitter, keeps the resulting declaration and map text in memory, and releases the temporary producer program. It does not require a preceding build or write generated files to disk.

This is a concrete declaration-boundary integration. It does not replace the parser or declaration emitter, make public declarations equivalent to implementation types, or automatically convert the normal editor source-reference mode into declaration checking.

## Dependency-ordered changes

| Step | Published revision | Change and practical effect |
| --- | --- | --- |
| 26 | [87f275782f](https://github.com/tomgasson/TypeScript/commit/87f275782fb54d29475aeb3ddd7adc38b9bba587) | Use one checker for single-file transpilation; add output/options regression coverage and cold API benchmarks. |
| 27 | [7fb6e44af5](https://github.com/tomgasson/TypeScript/commit/7fb6e44af580ffb29c621bdf3dac3da090098bf9) | Preserve negative directory dependencies when cloning editor programs, so a later file creation can invalidate a previously failed lookup. |
| 28 | [6f6a82e68e](https://github.com/tomgasson/TypeScript/commit/6f6a82e68e020667056606a2859ac2b2e5228410) | Read declaration visibility/default modifiers from AST flags, removing unnecessary resolver calls and checker locking. |
| 29 | [0b55e51a00](https://github.com/tomgasson/TypeScript/commit/0b55e51a00c2fcf7c3ac9102e7770b6db544cceb) | Use the normal compiler resolver directly when the project has no custom resolver. This also makes the default editor path distinguishable from unsupported custom resolver factories. |
| 30 | [a0156f2a3c](https://github.com/tomgasson/TypeScript/commit/a0156f2a3c19e4ab03648e47e0d70aff5b459028) | Refresh resolution watcher registrations after a full rebuild even when the final source-file names are unchanged. Newly discovered producer inputs and failed lookup candidates must be watched. |
| 31 | [5003382392](https://github.com/tomgasson/TypeScript/commit/5003382392eaac6985b6a391795016cf96ea0849) | Compute positions from the supplied source text when the host has no line map, including virtual files whose global path is ambiguous. |
| 32 | [8f2d999f1a](https://github.com/tomgasson/TypeScript/commit/8f2d999f1ac37bae5b45fcc154e9532227348b7d) | Integrate automatic declaration production, dependency ordering, successful-output availability, normal parse-cache ownership, generated maps, scoped language-service reads, and physical-output replacement. Includes compiler and editor regressions. |
| 33 | [135d0c1b8a](https://github.com/tomgasson/TypeScript/commit/135d0c1b8a6785b8dd2a571b59f5c40541a4adf2) | Extend real Session benchmarks with automatic generation, consumer edits, producer edits, source-read counters, and retained heap after edits. |
| 34 | [ce4066ccf9](https://github.com/tomgasson/TypeScript/commit/ce4066ccf93f1fb01cb19fca3fa69f079fa9c4c7) | Preserve the ordinary source/default path's fast path when no producer is eligible; avoid binding a loader callback unnecessarily. |
| 35 | [72e8095fa3](https://github.com/tomgasson/TypeScript/commit/72e8095fa3bd757d724afaafe7d234b740dc6e05) | Extract the private producer-emission boundary and test that retained output text does not keep the producer program, implementation ASTs, or checkers alive. |
| 36 | [043e5119b1](https://github.com/tomgasson/TypeScript/commit/043e5119b1d505381de5332d28332bcc12bbe984) | Cover missing-output dependency DAGs, resolved configuration overrides, invalidation and recovery after producer eligibility/signature changes. |
| 37 | [df9f0d60dd](https://github.com/tomgasson/TypeScript/commit/df9f0d60ddbd020887758837610a301ea8e31aad) | Include effective producer enum-preservation context in incremental declaration versions for isolated/verbatim consumers, covering unchanged declaration text and persisted build-info reuse. |
| 38 | [watch input tracking](https://github.com/tomgasson/TypeScript/tree/refactor/38-track-generated-declaration-watch-inputs) | Keep producer roots, referenced/extended configurations, wildcard additions, and hidden shallow inputs watched; refresh extended-config reads on full builds; cover failures and repairs. |
| 39 | [watch reuse](https://github.com/tomgasson/TypeScript/tree/refactor/39-reuse-generated-declarations-in-watch) | Ignore still-virtual declaration cache misses during a single consumer edit; retain physical-output precedence and force rebuilds for producer changes. |

## Activation and failure policy

The producer config must enable `isolatedDeclarations` and declaration emission (`declaration` or `composite`). For editor use, select the existing declaration boundary on the consumer:

```json
{
  "compilerOptions": { "disableSourceOfProjectReferenceRedirect": true },
  "references": [{ "path": "../producer" }]
}
```

No new config option is introduced. Existing physical declarations take precedence.


| Condition | Behavior |
| --- | --- |
| The consuming program normally redirects project references to source | Keep that source behavior. Automatic generation is inactive. |
| The consuming program uses declaration outputs | A missing mapped output may be synthesized. The compiler default already uses this route; editor consumers can select it through `disableSourceOfProjectReferenceRedirect`. |
| Producer `isolatedDeclarations` is false, even if the consumer enables it | Do not synthesize. Eligibility belongs to the resolved producer configuration. |
| Producer cannot emit declarations, sets `noEmit`, has configuration diagnostics, or uses content-mapper extensions | Do not synthesize. |
| Caller supplies a custom resolver factory, or requests `SkipModuleResolution` | Keep existing behavior; do not start automatic producer resolution. |
| Producer reference graph contains a cycle, including a reachable cyclic dependency | Do not synthesize that producer. Import cycles among files within one otherwise eligible project remain supported. |
| A physical declaration output exists | Use the existing output, including its existing failure behavior if it is unreadable. Generation does not silently replace it. |
| Producer configuration/program/syntax preflight or declaration emit fails | Discard that producer's captured output. Preserve the consumer's ordinary missing-output/file diagnostics. |
| Producer sets `noEmitOnError` | Honor it against the producer's declaration dependencies. The temporary producer does not force source redirects. |

Failure is not a fallback to the producer's implementation. In particular, invalid declaration extraction must not remove TS6053 for an explicitly referenced missing output or turn an unavailable dependency into a successful source check.

The feature does not presently surface a new detached producer-diagnostic payload in the consumer. Configuration or extraction failures can therefore leave the consumer reporting a missing output rather than explaining the producer's underlying error. Checking the producer directly still provides that explanation. When `noEmitOnError` is false, successful declaration emission is not a claim that the producer implementation has no type errors.

## Generation, identity, and ownership

Generation is initiated at the existing project-reference declaration lookup. A loader-local `sync.Once` ensures one attempt per resolved producer configuration during that consumer build. Eligible dependencies are processed first. Temporary producers use normal declaration dependencies over a read-only view of successful generated text and physical files; nested automatic generation is explicitly disabled inside each temporary producer.

The temporary host uses the consumer's tracked snapshot filesystem and the already resolved producer configurations. It acquires producer source trees independently, without leases in the consumer's parse cache. After a complete successful emit, only strings for missing outputs owned by that producer in the existing output mapper are published. Matching declaration maps are retained with those outputs. Producer diagnostics, emit metadata, checkers, and implementation ASTs are not part of the retained result.

The consumer parses generated text through its existing `GetSourceFileFromText` host boundary. Generated declaration ASTs therefore have the same parse-cache accounting as normal consumer files. Availability checks require successful generation; an eligible source file by itself is not treated as an existing declaration.

Output identity continues to use the existing source/output/config mapper. This preserves producer options needed by operations such as const-enum checking. Canonical-owner filtering prevents one producer's whole-project emit from overwriting another producer's chosen output at a shared path. Conflicting producer output layouts remain a limited case: the dependency graph follows declared references, while an overlapping output may belong to another canonical owner. Distinct output paths are the intended dependency-ordered case; full parity for conflicting build layouts is not claimed.

Language services read generated text through their exact Program's host. A snapshot-wide lookup returns text only when matching programs agree, rather than selecting an arbitrary representation. Generated maps override stale physical maps for the corresponding generated declaration. A declaration produced without a map does not borrow a stale or another project's map. Read-only resolver tests cover declaration and map aliases through symlinked packages, including map-first lookup.

## Editor invalidation and reuse

Consumer-only edits can retain the existing Program's generated text and reuse dependency declaration ASTs through normal Program cloning. Producer implementation/configuration inputs are read through tracked filesystem operations. Changes to those inputs require a new consumer build and regenerate declarations; there is no separate artifact cache spanning full Program rebuilds.

An external build may later create a physical file at a previously generated output path. Updating that generated file forces a full rebuild so the new physical declaration and its matching map policy replace the prior virtual representation. Reusing the old mapper while replacing only the AST would leave the language service reading stale text, so that clone path is explicitly rejected.

Watcher changes are necessary independently of generation: a rebuilt consumer can have identical final source-file names while its producer or module-resolution inputs change. The watcher regression checks actual client watcher registrations, not only a manually injected notification.

## Validation

`relevant-tests.log` records passing compiler, project, parser, build, incremental, command-line, transpile, API, checker, converter, compiler baseline, and fourslash packages. This is the broad relevant suite, not a claim that every repository package was run at the final revision.

`final-integration-tests.log` records the integrated feature passing all tests in the compiler, project, API, and converter packages. `generation-fast-path-tests.log` records the subsequent compiler fast-path check. The step-35 ownership regression passed 20 repetitions. Selected compiler, editor and transpile regressions passed ten repetitions under the race detector (`race-tests.log`); the converter package matched no tests in that race selection. `final-correctness-tests.log` records all tests passing in compiler, project, API, converters, incremental, build and command-line packages after steps 36–37. No baseline updates were required. The final stack rerun is in `final-stack-tests.log`; the isolated watch-safety step is recorded in `watch-safety-tests.log`. All nine new watch regressions and the incremental config regression passed ten repetitions under the race detector (`watch-context-race-tests.log`).

The regression corpus covers:

- Disk/generated diagnostic parity for private generic members, stripped exports, const enums, augmentations, source import cycles, and declaration maps.
- Producer/consumer option inversion, source redirects, custom resolvers, existing and unreadable output, invalid declaration/syntax/configuration, no-emit settings, and reference cycles.
- Concurrent requests for one producer, overlapping output ownership, explicit missing-file diagnostics, and producer `noEmitOnError` behavior against dependency declarations.
- Scoped snapshot reads, conflicting output paths across projects, declaration navigation, stale and absent maps, physical output replacement, and read-only symlink aliases.
- Real Session consumer edits, producer body/public/configuration/transitive changes, cache ownership, and output lifecycle transitions.

The step-35 weak-reference test retains successful declaration/map strings while dropping the emitting Program, and separately exercises rejected output. Its intended claim is collectability at that shared emission boundary. It does not claim that an active editor session releases all producer source text or all configured producer projects. The successful and rejected-output ownership cases passed; `project-reference-ownership-tests.log` records the run.

## CLI watch reuse

Steps 38–39 move the same declaration reuse into the existing CLI single-file update path. Generated declarations have no physical source-cache entry; counting them as changed previously forced complete producer regeneration alongside every consumer edit. The new regression performs two consumer edits and verifies fast-path Program cloning, identical declaration ASTs, unchanged producer read count, and correct diagnostics. This is a correctness/reuse assertion, not a wall-time benchmark.

Reuse required correcting input tracking first. The safety step covers referenced and extended configs, missing-config repair, shallow producer roots, coalesced directory events, new wildcard roots, hidden implementation-only dependencies, deletion and recreation, and generation failure followed by repair. Actual shallow inputs and previously watched still-observed dependencies remain tracked while absent speculative root lookups do not create root watches. Full builds refresh the extended-config cache so producer options and dependency reads stay current. Reference wildcard handling is restricted to producers enabling isolated declarations.

A physical declaration appearing on a consumer-edit event rejects reuse even if no separate output event was delivered. Producer input changes still require filesystem notification; changes with no delivered event are not a new polling mechanism. The safety-only branch passes its regressions before the reuse optimization is added. The final command-line suite passes with unchanged baselines.

## Incremental identity

Declaration text alone is insufficient for incremental diagnostic identity: the producer's effective `preserveConstEnums` setting changes whether an isolated/verbatim consumer may access ambient const enums. Step 37 adds a domain-tagged effective value to referenced declaration versions. Both values are tagged so older plain-text build-info versions invalidate once. Unchanged context retains diagnostic reuse; ordinary source versions and consumers without either flag retain their previous hashes. Tests cover both directions, generated/disk declarations, and warm/persisted state.

This fix applies when constructing an incremental Program. The `--build` orchestrator has separate timestamp and upstream-output skip decisions. It can conservatively rebuild touched tagged declarations because it compares plain text hashes, and unchanged output timestamps can bypass a producer-context comparison. Full context-aware solution-build skipping is not established here. Missing virtual declaration inputs also cause conservative repeated work in the physical-mtime up-to-date check. See `INCREMENTAL-NOTES.md`.

A further reproducer shows that a producer CommonJS/ESNext change can alter synthetic-default-import diagnostics with identical declaration text and file metadata. A candidate module-context version tag fixes warm and persisted incremental cases, but broad validation changes 81 baselines and makes existing solution-build cases rebuild where they previously only updated timestamps. That candidate is not in the code stack. `MODULE-CONTEXT.md` and `module-context-candidate.patch` preserve the reproduction and prerequisite work. Semantic declaration identity should be coordinated with the build orchestrator's content-version fast paths before extending the tag further.

## Measurements

The automatic route now reuses internally generated declarations across consumer edits. On this fixture, steady consumer edits are 8.1% faster and retain 84.0% less live heap than source dependencies. Producer edits expose the remaining cost directly: complete producer regeneration makes them about 22–23 times slower, despite preserving most declaration ASTs. The current implementation establishes ownership and invalidation; it does not yet make producer edits efficient.

The measured runtime is step 35. Steps 36–39 add regression coverage, CLI incremental context handling, and CLI watch behavior; they are outside the Session/transpile measurement binaries.

All results below are medians of six successful process samples per arm, collected in balanced order with `GOMAXPROCS=1`, `GOGC=100`, one-second work samples, and separate one-iteration heap samples. All 156 subprocess samples passed; collection took 333.2 seconds without overlapping builds or tests. `matrix/run-manifest.json` records exact production/harness revisions, source trees, build flags, binary hashes, sample order and exit status. `matrix/summary.json` contains every sample and comparison; `matrix/findings.md` contains the expanded tables. The analyzer verifies complete runs, expected benchmark names, finite metrics and matching metric sets.

Timing p values are exact two-sided Mann–Whitney label-permutation tests with tied ranks, enumerating all 924 assignments for six versus six observations. These are exploratory, unpaired comparisons without multiple-testing correction. A large p value does not establish equivalence, and these p values are not confidence intervals for median changes. Revision comparisons use identical benchmark harnesses; source/declaration mode comparisons share the same producer/consumer source fixture. Compiler/transpile binaries consistently use `-trimpath`; both project binaries omit it.

### Adjacent refactor controls

Step 25 versus 26 uses the same step-26 transpile benchmark. Both API modes construct a fresh single-file operation; the annotated sizes are one or 200 exported classes, each with an explicitly typed property and method.

| API / size | Time before → after | Timing change / p | Allocated bytes before → after | Allocations before → after |
| --- | ---: | ---: | ---: | ---: |
| Declaration / 1 | 267.310 → 151.863 µs | −43.2%; .00216 | 306,668.5 → 185,815.5 | 1,249 → 808 |
| Declaration / 200 | 3.129 → 3.214 ms | +2.7%; .485 | 1,945,364.5 → 1,775,133 | 15,406 → 14,962 |
| Module / 1 | 185.631 → 154.157 µs | −17.0%; .00216 | 136,515 → 153,782.5 | 1,195 → 1,191 |
| Module / 200 | 4.299 → 4.601 ms | +7.0%; .394 | 2,058,486 → 2,091,111 | 13,525 → 13,520 |

The declaration API avoids duplicated checker work: allocated bytes fall 39.4% for the tiny case and 8.75% for the larger case. The module API has a measured allocation tradeoff: bytes increase 12.65% and 1.58%, respectively, despite slightly fewer allocations. The larger inputs do not demonstrate a timing improvement in this run. Step 26 should not be described as an unconditional allocation win.

Step 27 versus 28 isolates syntax-only declaration modifiers using the same class fixture. For 200 annotated TS classes, uncached diagnostics change 0.967 → 0.977 ms (p=.699), and emit changes 1.546 → 1.515 ms (p=.310). For 200 full-signature JS classes, diagnostics change 1.641 → 1.603 ms (p=.0931), and emit changes 2.164 → 2.141 ms (p=.485). Across the one- and 200-class cases, allocation counts are unchanged; diagnostic bytes are unchanged and emit-byte medians differ by at most one byte. No timing gain is established by these controls. The change removes unnecessary resolver/locking dependencies and is useful preparation, rather than a measured throughput improvement here.

The source-only editor control compares exact production 25 against 35 using the same step-33 benchmark file. Median consumer-edit time changes 1.818 → 1.826 ms (+0.46%, p=.818); allocated bytes change 512,718.5 → 512,746.5 (p=.937), and allocations change 2,609 → 2,608. Retained heap changes 12,434,564 → 12,434,672 bytes/Session (+108 bytes, p=.818). This run does not demonstrate a source-route timing or retention regression; it is a bounded control on this fixture, not proof of zero overhead everywhere.

### Consumer reuse and producer invalidation

The real Session fixture has 48 producer modules with 256 body locals each, plus a barrel. Source, prepared declarations and automatic declarations use the same measured production revision 35. The project benchmark harness is frozen at revision 33 in both project binaries; only an unrelated physical-output regression requiring newer APIs is omitted from the mixed-version benchmark builds. No benchmark function differs between the source baseline and final binary.

| Consumer-edit mode | Time/edit | Allocated bytes/edit | Allocations/edit | Timing versus source |
| --- | ---: | ---: | ---: | ---: |
| Source dependencies | 1.792 ms | 512,717.5 | 2,608 | Reference |
| Prepared declarations | 1.647 ms | 526,161 | 2,945 | −8.12%; p=.00866 |
| Automatic declarations | 1.647 ms | 526,191.5 | 2,948 | −8.09%; p=.00216 |

Automatic and prepared declaration edit timing is similar in this run (p=.699); that does not prove equivalence. Automatic checking allocates 2.63% more bytes and 13.0% more objects per edit than source checking. The benefit is reduced retained dependency state, with a steady edit-time improvement on this implementation-heavy fixture.

The consumer-edit timer begins after opening and checking the Session, so initial automatic generation is outside it. The operation is an in-process partial edit, snapshot update, diagnostics request and background-work drain; it excludes LSP transport. Tests separately verify cloned Programs and unchanged dependency AST identities. All three arms report zero underlying producer-source reads during these consumer edits. This supports the tested reuse path but is not an extraction-call or artifact-cache-hit counter.

| Producer change | Source time/edit | Automatic time/edit | Time ratio | Source → automatic allocated bytes/edit |
| --- | ---: | ---: | ---: | ---: |
| Body only | 2.019 ms | 46.855 ms | 23.20× | 600,647.5 → 21,669,715 |
| Public return type | 2.004 ms | 46.277 ms | 23.09× | 605,432 → 21,686,247 |
| `declarationDir` | 2.167 ms | 47.958 ms | 22.13× | 573,843.5 → 21,995,952.5 |
| Transitive exported type | 2.027 ms | 45.222 ms | 22.31× | 610,776 → 21,700,973.5 |

All four producer timing comparisons have p=.00216. Automatic producer edits allocate about 35.5–38.3 times as many bytes as the source route. These are direct costs of rebuilding and emitting the complete producer on each invalidation. Body-only edits preserve all 49 declaration ASTs in the correctness test, but that reuse occurs after generation and does not avoid its elapsed time/allocation cost. This is the clearest measured reason for the next bounded, dependency-aware extraction cache or selective extraction step.

Producer operations include the file write, watched-file notification, consumer rebuild/check, background drain and any required generation. They alternate valid body, public-type, configuration or transitive-type states and verify diagnostics before timing. The transitive scenario changes a leaf public type while forwarding declaration text remains unchanged. Prepared declarations are absent from these comparisons because keeping their outputs unchanged would measure stale data.

Body/public/transitive arms each report two underlying producer-source reads per edit in both modes; configuration edits report zero. SnapshotFS can serve previously captured input text during complete regeneration. These counters therefore describe filesystem work, not regeneration counts, extraction hits/misses, or avoided parsing. There is no persistent extraction cache spanning full consumer rebuilds.

### Retained heap after edits

| Retained workload | Source bytes/Session | Prepared bytes/Session | Automatic bytes/Session | Automatic versus source |
| --- | ---: | ---: | ---: | ---: |
| 16 consumer edits | 12,434,550 | 952,134 | 1,986,468 | −84.02% |
| 16 body-only producer edits | 12,426,906 | Not measured | 2,567,814 | −79.34% |
| 16 public-type producer edits | 12,426,724 | Not measured | 2,568,518 | −79.33% |

Each source/automatic retention comparison has p=.00216. Prepared declarations retain 92.34% less heap than source in the consumer-edit experiment. Automatic generation retains more than prepared declarations: it owns per-Session generated output and producer-input tracking state; this experiment does not partition that difference by individual data structure. Automatic heap also rises after producer edits, while remaining far below source-mode heap. Every retained Session has 51 parse-cache entries; the tests independently check that changed old entries are removed and unchanged declaration entries are reused.

Each heap sample retains four Sessions after 16 edits each, releases old snapshots normally, forces GC, and reports marginal live heap per Session. These batches finish before the 30-second idle checker eviction timeout. The common source fixture and both edit-state strings exist before the baseline. Prepared declaration text is generated once and shared across Sessions; automatic output is generated independently after the baseline for each Session. Both declaration modes expose 6,296 logical declaration-text bytes per Session; only prepared mode reports those bytes as shared. Actual text storage and tracking ownership are included in the heap delta.

This is post-GC retained heap, not RSS, peak memory, total allocation, or a workload that keeps old snapshots alive. The ns/op and allocation totals printed by the heap benchmarks cover the complete four-Session setup/edit batch and should not be read as editor latency. The separate weak-reference ownership test addresses producer Program/AST/checker collectability; these heap measurements do not assert that all producer source text is released.

## Remaining limits and next steps

1. **Whole-producer work remains.** The first eligible missing output constructs, parses, and emits the producer project. It uses the ordinary parser and declaration emitter. Single-file extraction, direct signature IR, and body-skipping parsing remain future work.
2. **Peak producer memory remains.** Implementation ASTs and checkers exist during generation; independent producers can be generated concurrently. Tracked snapshot filesystem state can retain implementation text. Successful post-GC ownership tests do not imply lower cold peak memory.
3. **Producer edits regenerate.** There is no cross-snapshot artifact cache, per-file extraction cache, signature hash comparison, or dependency-driven selective re-extraction. Even body-only producer changes can rebuild and emit the producer again.
4. **Virtual directory enumeration is incomplete.** The resolver can recognize mapped generated outputs and directories, and language-service reads can retrieve them. Directory-entry enumeration remains physical, so path completions cannot yet enumerate every generated-only output directory/file.
5. **Unsupported contexts keep existing behavior.** Custom resolvers, content-mapped producers, cyclic project-reference graphs, invalid configurations, and metadata-only resolution mode are not silently approximated.
6. **Diagnostic transport needs a separate design.** Exposing producer failures in the consumer requires detached diagnostic locations/text, attribution, and lifetime rules; preserving missing-output behavior is the initial policy.
7. **Solution-build skipping remains incomplete.** Missing virtual input mtimes and producer semantic-context changes require separate up-to-date handling before diskless `--build` skipping is reliable.
8. **This is public declaration semantics.** Ordinary emitted declarations deliberately omit information such as private generic member types and can be affected by `stripInternal`, const enums, and side-effect imports. They are suitable at the existing declaration boundary. Replacing arbitrary source dependencies with internal summaries still needs the earlier signature-fidelity contract and differential corpus.

The next useful integration is a bounded artifact cache keyed by producer configuration, source content, dependency signatures, and output identity, with real Session invalidation tests. Per-file emission/extraction can then reduce cold and producer-edit work. A specialized signature parser and compact internal signature representation should follow demonstrated fidelity and invalidation, rather than precede them.
