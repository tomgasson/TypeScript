# Benchmark findings

Six process samples per arm; medians; exact two-sided Mann-Whitney label permutation including tied ranks; unpaired exploratory comparisons, no multiple-testing correction. P values test rank distributions, not confidence in a median difference or equivalence.

Heap ns/op and allocations include complete batches of four Sessions and 16 edits each; use custom live-B/session for retention. Post-GC marginal live heap is not RSS or peak memory. Shared fixture predates baseline; automatic outputs are generated separately per Session.

Producer source reads/bytes count successful underlying FS ReadFile operations; snapshotFS cached inputs can satisfy complete regeneration without additional underlying reads. These are not extraction counts or cache hit/miss counts.

## transpile

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkTranspile/Declaration/Annotated_1: 25 → 26 | ns/op | 267310 | 151863 | -43.19% | 0.00216 |
| BenchmarkTranspile/Declaration/Annotated_1: 25 → 26 | B/op | 306668 | 185816 | -39.41% | 0.00216 |
| BenchmarkTranspile/Declaration/Annotated_1: 25 → 26 | allocs/op | 1249 | 808 | -35.31% | 0.00216 |
| BenchmarkTranspile/Declaration/Annotated_200: 25 → 26 | ns/op | 3.12913e+06 | 3.21377e+06 | +2.70% | 0.48485 |
| BenchmarkTranspile/Declaration/Annotated_200: 25 → 26 | B/op | 1.94536e+06 | 1.77513e+06 | -8.75% | 0.00216 |
| BenchmarkTranspile/Declaration/Annotated_200: 25 → 26 | allocs/op | 15406 | 14962 | -2.88% | 0.00216 |
| BenchmarkTranspile/Module/Annotated_1: 25 → 26 | ns/op | 185631 | 154156 | -16.96% | 0.00216 |
| BenchmarkTranspile/Module/Annotated_1: 25 → 26 | B/op | 136515 | 153782 | +12.65% | 0.00216 |
| BenchmarkTranspile/Module/Annotated_1: 25 → 26 | allocs/op | 1195 | 1191 | -0.33% | 0.00216 |
| BenchmarkTranspile/Module/Annotated_200: 25 → 26 | ns/op | 4.29934e+06 | 4.60064e+06 | +7.01% | 0.39394 |
| BenchmarkTranspile/Module/Annotated_200: 25 → 26 | B/op | 2.05849e+06 | 2.09111e+06 | +1.58% | 0.00216 |
| BenchmarkTranspile/Module/Annotated_200: 25 → 26 | allocs/op | 13525 | 13520 | -0.04% | 0.00216 |

## modifiers

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmDiagnosticsUncached: 27 → 28 | ns/op | 10495.5 | 10956 | +4.39% | 0.06494 |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmDiagnosticsUncached: 27 → 28 | B/op | 7592 | 7592 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmDiagnosticsUncached: 27 → 28 | allocs/op | 128 | 128 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmEmit: 27 → 28 | ns/op | 18453.5 | 19486.5 | +5.60% | 0.09307 |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmEmit: 27 → 28 | B/op | 10367 | 10368 | +0.01% | 0.56710 |
| BenchmarkDeclarationClasses/AnnotatedTS_1/WarmEmit: 27 → 28 | allocs/op | 167 | 167 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmDiagnosticsUncached: 27 → 28 | ns/op | 967108 | 977316 | +1.06% | 0.69913 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmDiagnosticsUncached: 27 → 28 | B/op | 510392 | 510392 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmDiagnosticsUncached: 27 → 28 | allocs/op | 9001 | 9001 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmEmit: 27 → 28 | ns/op | 1.54585e+06 | 1.51513e+06 | -1.99% | 0.30952 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmEmit: 27 → 28 | B/op | 673016 | 673016 | -0.00% | 0.31602 |
| BenchmarkDeclarationClasses/AnnotatedTS_200/WarmEmit: 27 → 28 | allocs/op | 10069 | 10069 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmDiagnosticsUncached: 27 → 28 | ns/op | 14481 | 14657 | +1.22% | 0.48485 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmDiagnosticsUncached: 27 → 28 | B/op | 10434 | 10434 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmDiagnosticsUncached: 27 → 28 | allocs/op | 155 | 155 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmEmit: 27 → 28 | ns/op | 23368 | 23130 | -1.02% | 0.93723 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmEmit: 27 → 28 | B/op | 13203 | 13203 | +0.00% | 0.86364 |
| BenchmarkDeclarationClasses/JSFullSignature_1/WarmEmit: 27 → 28 | allocs/op | 194 | 194 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmDiagnosticsUncached: 27 → 28 | ns/op | 1.64063e+06 | 1.60263e+06 | -2.32% | 0.09307 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmDiagnosticsUncached: 27 → 28 | B/op | 947370 | 947370 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmDiagnosticsUncached: 27 → 28 | allocs/op | 11948 | 11948 | +0.00% | 1.00000 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmEmit: 27 → 28 | ns/op | 2.16395e+06 | 2.14136e+06 | -1.04% | 0.48485 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmEmit: 27 → 28 | B/op | 1.08268e+06 | 1.08268e+06 | +0.00% | 0.80303 |
| BenchmarkDeclarationClasses/JSFullSignature_200/WarmEmit: 27 → 28 | allocs/op | 12966 | 12966 | +0.00% | 1.00000 |

## source-overhead

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationBoundary: 25 → final | ns/op | 1.81785e+06 | 1.82619e+06 | +0.46% | 0.81818 |
| BenchmarkEditorDeclarationBoundary: 25 → final | B/op | 512718 | 512746 | +0.01% | 0.93723 |
| BenchmarkEditorDeclarationBoundary: 25 → final | allocs/op | 2609 | 2608 | -0.04% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: 25 → final | producer-source-reads/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundary: 25 → final | producer-source-read-B/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: 25 → final | live-B/session | 1.24346e+07 | 1.24347e+07 | +0.00% | 0.81818 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: 25 → final | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: 25 → final | declaration-text-B/session | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: 25 → final | shared-declaration-B | 0 | 0 | +0 | 1.00000 |

## consumer-reuse

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → PreparedDeclarations | ns/op | 1.7923e+06 | 1.64669e+06 | -8.12% | 0.00866 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → PreparedDeclarations | B/op | 512718 | 526161 | +2.62% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → PreparedDeclarations | allocs/op | 2608 | 2945 | +12.92% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → PreparedDeclarations | producer-source-reads/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → PreparedDeclarations | producer-source-read-B/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → PreparedDeclarations | live-B/session | 1.24346e+07 | 952134 | -92.34% | 0.00216 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → PreparedDeclarations | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → PreparedDeclarations | declaration-text-B/session | 0 | 6296 | +6296 | 0.00216 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → PreparedDeclarations | shared-declaration-B | 0 | 6296 | +6296 | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → AutomaticDeclarations | ns/op | 1.7923e+06 | 1.64726e+06 | -8.09% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → AutomaticDeclarations | B/op | 512718 | 526192 | +2.63% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → AutomaticDeclarations | allocs/op | 2608 | 2948 | +13.04% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → AutomaticDeclarations | producer-source-reads/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundary: SourceDependencies → AutomaticDeclarations | producer-source-read-B/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → AutomaticDeclarations | live-B/session | 1.24346e+07 | 1.98647e+06 | -84.02% | 0.00216 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → AutomaticDeclarations | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → AutomaticDeclarations | declaration-text-B/session | 0 | 6296 | +6296 | 0.00216 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: SourceDependencies → AutomaticDeclarations | shared-declaration-B | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundary: PreparedDeclarations → AutomaticDeclarations | ns/op | 1.64669e+06 | 1.64726e+06 | +0.03% | 0.69913 |
| BenchmarkEditorDeclarationBoundary: PreparedDeclarations → AutomaticDeclarations | B/op | 526161 | 526192 | +0.01% | 0.58874 |
| BenchmarkEditorDeclarationBoundary: PreparedDeclarations → AutomaticDeclarations | allocs/op | 2945 | 2948 | +0.10% | 0.00216 |
| BenchmarkEditorDeclarationBoundary: PreparedDeclarations → AutomaticDeclarations | producer-source-reads/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundary: PreparedDeclarations → AutomaticDeclarations | producer-source-read-B/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: PreparedDeclarations → AutomaticDeclarations | live-B/session | 952134 | 1.98647e+06 | +108.63% | 0.00216 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: PreparedDeclarations → AutomaticDeclarations | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: PreparedDeclarations → AutomaticDeclarations | declaration-text-B/session | 6296 | 6296 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationBoundaryRetainedHeap: PreparedDeclarations → AutomaticDeclarations | shared-declaration-B | 6296 | 0 | -100.00% | 0.00216 |

## producer-BodyOnly

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationProducerChanges/BodyOnly: SourceDependencies → AutomaticDeclarations | ns/op | 2.01947e+06 | 4.68553e+07 | +2220.18% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/BodyOnly: SourceDependencies → AutomaticDeclarations | B/op | 600648 | 2.16697e+07 | +3507.73% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/BodyOnly: SourceDependencies → AutomaticDeclarations | allocs/op | 2666 | 220612 | +8175.00% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/BodyOnly: SourceDependencies → AutomaticDeclarations | producer-source-reads/op | 2 | 2 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerChanges/BodyOnly: SourceDependencies → AutomaticDeclarations | producer-source-read-B/op | 23334 | 23334 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerRetainedHeap/BodyOnly: SourceDependencies → AutomaticDeclarations | live-B/session | 1.24269e+07 | 2.56781e+06 | -79.34% | 0.00216 |
| BenchmarkEditorDeclarationProducerRetainedHeap/BodyOnly: SourceDependencies → AutomaticDeclarations | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerRetainedHeap/BodyOnly: SourceDependencies → AutomaticDeclarations | declaration-text-B/session | 0 | 6296 | +6296 | 0.00216 |
| BenchmarkEditorDeclarationProducerRetainedHeap/BodyOnly: SourceDependencies → AutomaticDeclarations | shared-declaration-B | 0 | 0 | +0 | 1.00000 |

## producer-PublicSignature

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationProducerChanges/PublicSignature: SourceDependencies → AutomaticDeclarations | ns/op | 2.0041e+06 | 4.62768e+07 | +2209.10% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/PublicSignature: SourceDependencies → AutomaticDeclarations | B/op | 605432 | 2.16862e+07 | +3481.95% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/PublicSignature: SourceDependencies → AutomaticDeclarations | allocs/op | 2750 | 220744 | +7927.05% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/PublicSignature: SourceDependencies → AutomaticDeclarations | producer-source-reads/op | 2 | 2 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerChanges/PublicSignature: SourceDependencies → AutomaticDeclarations | producer-source-read-B/op | 23343 | 23343 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerRetainedHeap/PublicSignature: SourceDependencies → AutomaticDeclarations | live-B/session | 1.24267e+07 | 2.56852e+06 | -79.33% | 0.00216 |
| BenchmarkEditorDeclarationProducerRetainedHeap/PublicSignature: SourceDependencies → AutomaticDeclarations | cache-entries/session | 51 | 51 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerRetainedHeap/PublicSignature: SourceDependencies → AutomaticDeclarations | declaration-text-B/session | 0 | 6296 | +6296 | 0.00216 |
| BenchmarkEditorDeclarationProducerRetainedHeap/PublicSignature: SourceDependencies → AutomaticDeclarations | shared-declaration-B | 0 | 0 | +0 | 1.00000 |

## producer-ProducerConfig

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationProducerChanges/ProducerConfig: SourceDependencies → AutomaticDeclarations | ns/op | 2.16711e+06 | 4.79584e+07 | +2113.01% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/ProducerConfig: SourceDependencies → AutomaticDeclarations | B/op | 573844 | 2.1996e+07 | +3733.09% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/ProducerConfig: SourceDependencies → AutomaticDeclarations | allocs/op | 5383 | 223462 | +4051.24% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/ProducerConfig: SourceDependencies → AutomaticDeclarations | producer-source-reads/op | 0 | 0 | +0 | 1.00000 |
| BenchmarkEditorDeclarationProducerChanges/ProducerConfig: SourceDependencies → AutomaticDeclarations | producer-source-read-B/op | 0 | 0 | +0 | 1.00000 |

## producer-TransitiveSignature

| Case / arms | Metric | Before median | After median | Change | Exact p |
| --- | --- | ---: | ---: | ---: | ---: |
| BenchmarkEditorDeclarationProducerChanges/TransitiveSignature: SourceDependencies → AutomaticDeclarations | ns/op | 2.02685e+06 | 4.52219e+07 | +2131.14% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/TransitiveSignature: SourceDependencies → AutomaticDeclarations | B/op | 610776 | 2.1701e+07 | +3453.02% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/TransitiveSignature: SourceDependencies → AutomaticDeclarations | allocs/op | 2753 | 220836 | +7921.63% | 0.00216 |
| BenchmarkEditorDeclarationProducerChanges/TransitiveSignature: SourceDependencies → AutomaticDeclarations | producer-source-reads/op | 2 | 2 | +0.00% | 1.00000 |
| BenchmarkEditorDeclarationProducerChanges/TransitiveSignature: SourceDependencies → AutomaticDeclarations | producer-source-read-B/op | 23427 | 23427 | +0.00% | 1.00000 |
