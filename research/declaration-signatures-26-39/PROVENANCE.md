# Measured and published revisions

Original measured build and run manifests are unchanged and retain local commit IDs and workspace paths. Published commits differ in commit identity but the publication record has identical full Git trees, checked here against every local commit. The mapping does not retime or relabel measurements.

| Step | Measured local commit | Published fork commit | Full tree |
| --- | --- | --- | --- |
| 25 | `19e34233bfe1aadb262d4a4b61b5dd51207a7c8f` | [`19e34233bfe1aadb262d4a4b61b5dd51207a7c8f`](https://github.com/tomgasson/TypeScript/commit/19e34233bfe1aadb262d4a4b61b5dd51207a7c8f) | `1be0a262665cea7f13542c71fe25c5ef250a52c3` |
| 26 | `cda2c80535c31e0824392816a23f952ad334e545` | [`87f275782fb54d29475aeb3ddd7adc38b9bba587`](https://github.com/tomgasson/TypeScript/commit/87f275782fb54d29475aeb3ddd7adc38b9bba587) | `2fffa5ecb65a7bd33c06830e48073745323a4213` |
| 27 | `73dfc32cb10de002dba43d76655259ca64315ff4` | [`7fb6e44af580ffb29c621bdf3dac3da090098bf9`](https://github.com/tomgasson/TypeScript/commit/7fb6e44af580ffb29c621bdf3dac3da090098bf9) | `451d438ae0b83548c9881230c135d2495ed88fd3` |
| 28 | `5aac8cbaccd3cbcb895a243ae7f0ba75e8dc4723` | [`6f6a82e68e020667056606a2859ac2b2e5228410`](https://github.com/tomgasson/TypeScript/commit/6f6a82e68e020667056606a2859ac2b2e5228410) | `c635457e8b279d02b4589a8302692f53510d70f6` |
| 29 | `5987816acdf6c5b425f5c0eb07a01307102c3f50` | [`0b55e51a00c2fcf7c3ac9102e7770b6db544cceb`](https://github.com/tomgasson/TypeScript/commit/0b55e51a00c2fcf7c3ac9102e7770b6db544cceb) | `4887a9a995e3f0c60ad060fb4aa835d419f146d5` |
| 30 | `041ea7a27043966afa11d67c64c889c95cc943be` | [`a0156f2a3c19e4ab03648e47e0d70aff5b459028`](https://github.com/tomgasson/TypeScript/commit/a0156f2a3c19e4ab03648e47e0d70aff5b459028) | `16ff57e021aea005d8b05b3386af57b46c9bbe7d` |
| 31 | `08aa0ea4765254f358b3aacce3a4c5a7751b738a` | [`5003382392eaac6985b6a391795016cf96ea0849`](https://github.com/tomgasson/TypeScript/commit/5003382392eaac6985b6a391795016cf96ea0849) | `21d7543d3e95e1df2f37f5e1af147e931571b9b2` |
| 32 | `d54767aeb109696febfa029416c7c02164dc743e` | [`8f2d999f1ac37bae5b45fcc154e9532227348b7d`](https://github.com/tomgasson/TypeScript/commit/8f2d999f1ac37bae5b45fcc154e9532227348b7d) | `752498c898b0c6b8bdab9babb3d29c544318c3b5` |
| 33 | `6496147f56a566562a1b1cc2f735d60276ae9d48` | [`135d0c1b8a6785b8dd2a571b59f5c40541a4adf2`](https://github.com/tomgasson/TypeScript/commit/135d0c1b8a6785b8dd2a571b59f5c40541a4adf2) | `769c47f073c0f2c3cb46cd6859db2b7f0581d40b` |
| 34 | `2d2fd2ab2b46eec9d20583280a4ea3ae81905308` | [`ce4066ccf93f1fb01cb19fca3fa69f079fa9c4c7`](https://github.com/tomgasson/TypeScript/commit/ce4066ccf93f1fb01cb19fca3fa69f079fa9c4c7) | `06ea81e2d182daa3c5388a08f8f68b78f55a695a` |
| 35 | `52121ea757d9259238d5362ddf3188f82cb80297` | [`72e8095fa3bd757d724afaafe7d234b740dc6e05`](https://github.com/tomgasson/TypeScript/commit/72e8095fa3bd757d724afaafe7d234b740dc6e05) | `f420672d0070240847f0938e68319bc9fdc660b2` |

The measured endpoint is step35, published as `72e8095fa3bd757d724afaafe7d234b740dc6e05`. Later changes are not included in these measurements. Baseline25 is `19e34233bfe1aadb262d4a4b61b5dd51207a7c8f` in both locations.

Project benchmarks use production25/35, package tests25, and the shared project benchmark file33. The named physical-output regression is omitted in both benchmark binaries because it calls newer APIs; benchmark functions remain unchanged.

Rebuilt binary hashes may differ after relocating paths or rebuilding from the published commit metadata. Match source/harness trees and hashes, flags and toolchain; do not edit the archived binary hashes to make a new run appear original.
