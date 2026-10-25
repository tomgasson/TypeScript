# Incremental integration review

Code review, 2026-10-25. The narrow diagnostic-cache regression was executed before the measurement window, with permission. The build-mode and watch observations remain code-derived. They are separate from the measured performance results.

## File versions and diagnostics

`tsc/internal/execute/incremental/programtosnapshot.go` computes each file version from `SourceFile.Text()` (with the existing content-mapper exception), rather than `SourceFile.Hash` or a physical-file read. `affectedfileshandler.go` uses the version as the shape signature for declaration inputs. Consequently, newly generated declaration text participates in ordinary incremental invalidation, even though the declaration has never existed on disk. Build-info serialization retains that version and the declaration path; loading build info reconstructs state without requiring that input to exist physically.

Before the fix below, diagnostic-cache identity did not include referenced-project compiler options. It compared consumer options, input text, global-scope effects, implied node format, and referenced path sets. A producer `preserveConstEnums` change can leave declaration text unchanged while changing whether an `isolatedModules` consumer must report TS2748. This also affected existing disk declarations; it was not introduced by virtual declarations.

The new test `tsc/internal/execute/incremental/projectreference_config_test.go` compares reused incremental state with a fresh build in both directions (`preserveConstEnums` enabled/disabled), using both generated and disk declarations. It explicitly verifies unchanged declaration text. The targeted run reproduced all four failures: false to true retains a stale TS2748, while true to false omits a new TS2748. Both storage modes fail identically. The package test execution completed in 0.011 seconds; this is a correctness test result, not a performance sample.

After the measurement matrix completed, a narrow fix added the effective producer `ShouldPreserveConstEnums` bit to referenced declaration versions, only for consumers enforcing `isolatedModules` or `verbatimModuleSyntax`. Both bit values receive a domain tag, invalidating historical plain-text versions once. Ordinary source versions and consumers without either flag retain the existing hashes. No build-info schema changes or general configuration serialization were added.

The expanded targeted regression passed in 0.035 seconds, covering both declaration storage modes, both option-change directions, both consumer flags and a neither-flag control, plus in-memory and persisted build-info reuse. Unchanged context retains an empty changed-file set and cached consumer diagnostics. These correctness runs are not performance samples and this subsequent fix was not part of the completed step-35 performance matrix.

The build-mode physical-mtime check compares stored versions with a plain text hash. A tagged referenced-declaration version therefore cannot use its unchanged-text shortcut after that input's timestamp advances; this conservatively schedules a rebuild. Warm and persisted incremental reuse still work when the regenerated text and effective producer context are unchanged, as tested above.

Targeted command from `ts-small`:

```sh
/workspace/scratch/610fb4329ae2/toolchain/go/bin/go -C tsc test -tags=embed -p=2 ./internal/execute/incremental -run '^TestProjectReferenceConfigChangeInvalidatesDiagnostics$' -count=1 -timeout=45s
```

## Build-mode up-to-date checks still require physical inputs

`tsc/internal/execute/build/buildtask.go`, in `getUpToDateStatus`, enumerates non-root build-info inputs and obtains their physical modification times. A zero time returns `upToDateStatusTypeInputFileMissing`. The explicit virtual-input exception handles content-mapper supplemental files, not synthesized project-reference declarations. The resulting verbose status says the input does not exist and schedules a build; it is not a type-checking error or an unsafe up-to-date skip.

An exact scenario to test is:

1. Build a composite producer and consumer successfully, leaving their build-info files.
2. Delete only the producer declaration output, preserving its source and build info.
3. Build again so the consumer loads the missing declaration through automatic extraction.
4. Repeat the build without changing any source.

The producer's ordinary-output timestamp loop runs only for non-incremental projects. If it remains up to date, the consumer's build info still names the absent physical declaration and the non-root input check requests another build. This is a code-derived repeated-work risk, not a runtime-verified result. Do not claim diskless `--build` up-to-date skipping is supported or measured by the current compiler/editor benchmarks.

## CLI watch behavior

Producer source/config reads pass through the watcher's tracking filesystem. A changed seen file that is not a consumer Program source forces a full compiler Program rebuild. This prevents the producer implementation from being mistaken for an unchanged generated declaration.

The CLI watch source cache uses physical modification times and does not store supplied-text declaration parses. Its single-file fast path therefore sees generated declaration entries as cache misses. A consumer source edit adds another miss, causing a full rebuild. This is conservative extra work, separate from the language-service snapshot implementation and from any stale-diagnostic issue above.
