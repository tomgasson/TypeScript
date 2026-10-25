Start with [RESULTS.md](RESULTS.md) for changes, measurements, and limits. [published-26-39.json](published-26-39.json) records every branch and its exact published/source tree. [PORTABLE-REPRODUCE.md](PORTABLE-REPRODUCE.md) rebuilds the measured variants from published commits.

The measured endpoint is step 35; later correctness and watch changes are separately validated. The module-context candidate is deliberately unshipped and documented in [MODULE-CONTEXT.md](MODULE-CONTEXT.md). Its failing logs are retained as prerequisite evidence, not final-stack failures.

`PACKAGE-MANIFEST.json` inventories the original measurement package; `ARCHIVE-MANIFEST.json` inventories the complete final evidence archive.
