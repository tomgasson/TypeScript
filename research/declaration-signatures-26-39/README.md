# Measurement evidence: automatic declarations, steps26–35

This staging bundle contains the original completed performance matrix and enough provenance/scripts to rebuild the compared source and shared harnesses. It intentionally excludes benchmark binaries, generated source-overlay directories, overlay JSON files, Go caches, and duplicate implementation payloads. The narrative RESULTS.md and correctness/race validation logs are maintained separately by the report owner.

- `matrix/`:26 raw stdout files, original run manifest, validated summary and expanded findings. All156 subprocess samples passed, all8 groups completed.
- `variants-*/build-variants.json` and `*.build.log`: original build provenance and logs for the six binaries. Absolute paths and local source IDs are preserved as recorded; binaries/overlays are not bundled.
- `build-plan-*.json`: original resolved build commands.
- `published-26-35.json`, `PROVENANCE.json`, `PROVENANCE.md`: original publication mapping and compact measured-local to published-fork mapping. The publication records' full trees match all11 locally checked source commits, including baseline25.
- `build_variants.py`, `build_all.py`, `run_matrix.py`, `analyze_matrix.py`: reproduction and analysis code. The runner includes a post-run guard against overwriting an existing manifest when selecting a disjoint group; this does not change benchmark commands or archived data.
- `PORTABLE-REPRODUCE.md`: commands using published immutable commits, explicit local paths and a new output directory. `REPRODUCE.md` retains the original workspace-oriented procedure and measurement limits.
- `PACKAGE-MANIFEST.json`: archive file sizes and SHA-256 checksums; also identifies files copied byte-for-byte from the measurement workspace. Its own checksum is intentionally excluded.

The measured production endpoint is step35, published as `72e8095fa3bd757d724afaafe7d234b740dc6e05`. Later runtime changes are not included. The project benchmark file is step33, published as `135d0c1b8a6785b8dd2a571b59f5c40541a4adf2`; the original manifests retain its measured local identity. The baseline25 commit is identical locally and in the fork.

All timings are elapsed wall time. Heap samples measure post-GC marginal retained heap, not RSS or peak memory. Source-read counts are underlying filesystem operations, not extraction-call or cache-hit counts. Full producer regeneration is included in producer-edit samples; initial generation is excluded from steady consumer-edit timing. See the included method notes before interpreting results.
