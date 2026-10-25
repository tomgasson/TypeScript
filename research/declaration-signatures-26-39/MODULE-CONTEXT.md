# Producer module context: confirmed incremental prerequisite

The producer's module configuration can change consumer diagnostics without changing declaration text or the declaration's ordinary `SourceFileMetaData`. This remains a known limitation after the narrow const-enum context fix in step 37. The module-context candidate was tested and deliberately deferred; it is not part of the published implementation.

## Reproducer and evidence

The consumer uses `module: esnext`, `moduleResolution: bundler`, and a synthetic default import of a referenced producer's named export. It does not enable `isolatedModules` or `verbatimModuleSyntax`. The producer exports an explicitly annotated value and switches only between `module: commonjs` and `module: esnext`.

Both producer configurations supply identical declaration text, `export declare const named: number;`, and identical ordinary declaration metadata. A fresh consumer permits the synthetic default for the CommonJS producer and reports TS1192 for the ESNext producer. An incremental consumer previously reused the old result in both directions.

The new regression exercised disk and automatically generated declarations, both direction changes, and both in-memory and persisted-build-info state. All eight change cases failed before the candidate; unchanged configuration reused diagnostics correctly. The complete failures are in `module-context-before.log`.

The responsible checker path is `canHaveSyntheticDefault`. For a declaration with no implied emit format, it consults the referenced producer through `GetEmitModuleFormatOfFile`. The three callers do not guard this operation on `AllowSyntheticDefaultImports`; narrowing the fix to that option would not match the implementation.

## Candidate and validation

`module-context-candidate.patch` is a complete candidate against local step-37 revision `1fa8f7acebbfeac9c3fe8f6c638828061996e740`. The published step-37 commit is `df9f0d60ddbd020887758837610a301ea8e31aad` with the identical source tree, so the patch applies there too. It includes the regression file and the corresponding changes to the existing const-enum test. It adds the producer's effective emit-module format to the declaration version context for all mapped reference outputs, retaining the effective `preserveConstEnums` component only for isolated/verbatim consumers. The combined context is hashed once with the base content version.

The requested validation command was:

```sh
go -C tsc test -tags=embed ./internal/execute/incremental ./internal/execute/build ./internal/execute/tsctests -count=1
```

`module-context-after.log` records:

- Incremental package passed, including all eight reproduced stale-diagnostic cases.
- Build package passed.
- Command-line baseline package failed with 81 changed baseline files. No baselines were accepted.

Of the 81 baseline changes, 63 contained only version/signature/hash and serialized-size line changes. Eighteen had other changed lines: sixteen solution-building variants changed from timestamp-only pseudo-builds to full builds, one program-update baseline gained a checking/signature summary, and one module-specifier baseline had surrounding metadata formatting changes. The sixteen build behavior changes are a real performance regression, not merely updated expected hashes.

## Why the candidate was deferred

`BuildTask.getUpToDateStatus` compares `FileInfo.Version()` to a plain hash of current file text when an input timestamp is newer. Context-tagged versions no longer match that text-only hash even when the file and producer context are unchanged. This defeats the existing timestamp-only fast path.

A separate issue remains even if those hashes are made compatible: a producer configuration edit can leave declaration text and declaration timestamps unchanged. `skipDtsOutputOfComposite` avoids rewriting identical declarations, and the solution builder can classify the consumer as up to date with upstream types before constructing an incremental Program. A context fix that runs only during Program construction cannot invalidate diagnostics in a Program that the solution builder never constructs.

These paths need an explicit semantic-context identity contract before the broader module-context fix is shipped. The current narrow step-37 fix remains intact; this report does not claim complete solution-build handling of producer-only configuration changes.

## Smallest useful follow-up

One option is a shared file-version helper used by both incremental snapshots and solution-build checks, with the current producer context obtained from the same resolved output-owner mapping. Comparing producer context must also happen before an unchanged-declaration pseudo-build. The build path must not merely strip the context tag or accept either context value: that would hide the actual configuration change.

Another option is to retain the content-only file version used by the timestamp checker and place producer context in the declaration's semantic signature. That requires coordinated changes to initial change detection and `affectedFilesHandler.updateShapeSignature`, so context changes propagate to consumers without overwriting the old signature prematurely. It also requires a separate solution-build context check.

Required regressions are the captured module test, the existing const-enum transitions, unchanged context reuse, touched-but-identical declaration timestamps, and configuration-only solution builds with unchanged declaration bytes/timestamps. Review baseline behavior before accepting updated snapshots. A parser or internal signature representation will need this context dependency regardless of whether declarations are held on disk or in memory.
