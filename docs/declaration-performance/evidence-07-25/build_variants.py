#!/usr/bin/env python3
"""Freeze compiler/parser benchmark overlays; build serially only with --build.

Examples:
    python build_variants.py 06 20
    python build_variants.py 06 10 11 12 13 14 15 19 20 --build
    python build_variants.py 22 23 24 --test-ref refactor/24-skip-single-declaration-overload-probes --output-dir variants-22-24 --build
    python build_variants.py 20 21 --package parser --test-ref refactor/21-release-pooled-scanner-inputs --output-dir variants-parser --no-trimpath --build

Production comes from each exact revision. Package tests come from the common
harness revision (20 by default), except declaration_bench_test.go, whose private
newEmitHost call changed in 20. Extra/uncommitted package tests are excluded. These
mixed-version binaries are for benchmarks with -test.run=^$, not for running new
regression tests against old implementations.

Parser fixture initialization deliberately rejects -trimpath. Use --no-trimpath
for BOTH parser variants and compare only within that pair. Compiler variants
retain the default -trimpath setting for consistency with the original binaries.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import time


HERE = Path(__file__).resolve().parent
DEFAULT_REPO = next((p for p in HERE.parents if (p / "tsc/go.mod").is_file()), Path.cwd())
REFS = {
    "06": "refactor/06-skip-inactive-commonjs-scan",
    "10": "refactor/10-clear-nodebuilder-context-stack",
    "11": "refactor/11-skip-absent-declaration-parameters",
    "12": "refactor/12-project-reference-provenance",
    "13": "refactor/13-release-completed-emitters",
    "14": "refactor/14-stable-diagnostic-signatures",
    "15": "refactor/15-emitted-source-identity",
    "19": "refactor/19-declaration-boundary-benchmarks",
    "20": "refactor/20-explicit-emit-checker-lease",
    "21": "refactor/21-release-pooled-scanner-inputs",
    "22": "refactor/22-host-owned-parsing-from-text",
    "23": "refactor/23-lazy-index-signature-builder",
    "24": "refactor/24-skip-single-declaration-overload-probes",
}
DEFAULT_VARIANTS = ["06", "10", "11", "12", "13", "14", "15", "19", "20"]
VERSIONED_TEST = "tsc/internal/compiler/declaration_bench_test.go"


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def save_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("variants", nargs="*", choices=list(REFS), help="default: original compiler variants 06,10,11,12,13,14,15,19,20")
    parser.add_argument("--build", action="store_true", help="build prepared variants serially; never runs benchmarks")
    parser.add_argument("--package", choices=["compiler", "parser"], default="compiler")
    parser.add_argument("--no-trimpath", action="store_true", help="required for parser filesystem fixtures; use consistently within a comparison")
    parser.add_argument("--repo", type=Path, default=DEFAULT_REPO)
    parser.add_argument("--output-dir", type=Path, default=None)
    parser.add_argument("--go", default="go")
    parser.add_argument("--gopath", type=Path, default=None)
    parser.add_argument("--test-ref", "--harness-ref", dest="test_ref", default=REFS["20"])
    args = parser.parse_args()
    variants = list(dict.fromkeys(args.variants or DEFAULT_VARIANTS))
    package_dir = "tsc/internal/" + args.package + "/"
    package_target = "./internal/" + args.package
    versioned_tests = [VERSIONED_TEST] if args.package == "compiler" else []
    repo = args.repo.resolve()
    default_directory = "variants-parser" if args.package == "parser" else "variants"
    output = (args.output_dir or repo / "_declaration-perf" / default_directory).resolve()
    executable = shutil.which(args.go)
    if executable is None:
        parser.error("Go executable not found: " + args.go)
    go = Path(executable).resolve()
    output.mkdir(parents=True, exist_ok=True)

    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(repo), *arguments])

    def resolve(ref):
        for candidate in [ref, "refs/remotes/origin/" + ref]:
            result = subprocess.run(["git", "-C", str(repo), "rev-parse", "--verify", candidate + "^{commit}"], capture_output=True, text=True)
            if result.returncode == 0:
                return result.stdout.strip()
        raise RuntimeError("Missing revision; fetch the fork branches first: " + ref)

    def tree(revision):
        return git("rev-parse", revision + "^{tree}").decode().strip()

    def names(*arguments):
        return {name.decode() for name in git(*arguments).split(b"\0") if name}

    def revision_files(revision):
        return names("ls-tree", "-r", "--name-only", "-z", revision, "--", "tsc")

    def production(name):
        return name.endswith(".go") and not name.endswith("_test.go")

    def package_test(name):
        return name.startswith(package_dir) and "/" not in name[len(package_dir):] and name.endswith("_test.go")

    harness = resolve(args.test_ref)
    revisions = {name: resolve(REFS[name]) for name in variants}
    base = resolve(REFS["06"])
    harness_files = revision_files(harness)
    test_files = sorted(name for name in harness_files if package_test(name))

    # Include dirty/later source files as well as changes since 06: unrelated
    # ongoing parser/host work must not silently leak into an old comparison.
    changed = names("diff", "--name-only", "-z", base, harness, "--", "tsc")
    for revision in revisions.values():
        changed |= names("diff", "--name-only", "-z", harness, revision, "--", "tsc")
    changed |= names("diff", "--name-only", "-z", harness, "--", "tsc")
    changed |= names("ls-files", "--others", "--exclude-standard", "-z", "--", "tsc")
    source_files = sorted(name for name in changed if production(name))

    # Freeze every test in the requested package, including unchanged ones.
    # Other packages' tests are not compiled by this command. Their production
    # files are overlaid normally, without a special exclusion.
    common = output / "common-tests"
    common.mkdir(exist_ok=True)
    common_replacements = {}
    test_hashes = {}
    for name in test_files:
        if name in versioned_tests:
            continue
        data = git("show", harness + ":" + name)
        destination = common / Path(name).name
        destination.write_bytes(data)
        common_replacements[str(repo / name)] = str(destination)
        test_hashes[name] = sha256(data)
    extra_tests = sorted(
        str(path.relative_to(repo))
        for path in (repo / package_dir).glob("*_test.go")
        if str(path.relative_to(repo)) not in harness_files
    )
    for name in extra_tests:
        common_replacements[str(repo / name)] = ""

    # The overlay doesn't replace non-Go inputs. Refuse module/embedded-input
    # drift instead of pretending these are complete independent checkouts.
    def non_go_inputs_match(revision):
        differences = names("diff", "--name-only", "-z", revision, "--", "tsc")
        unexpected = sorted(name for name in differences if not name.endswith(".go"))
        if unexpected:
            raise RuntimeError("Non-Go worktree inputs differ from " + revision + ": " + ", ".join(unexpected))

    for revision in revisions.values():
        non_go_inputs_match(revision)

    # Pin the toolchain and its build inputs; no auto toolchain/network upgrade.
    env = dict(os.environ)
    env.update({
        "PATH": str(go.parent) + os.pathsep + env.get("PATH", ""),
        "GOTOOLCHAIN": "local",
        "GOWORK": "off",
        "GOFLAGS": "",
        "GOMAXPROCS": "2",
    })
    if args.gopath is not None:
        env["GOPATH"] = str(args.gopath.resolve())
    go_version = subprocess.check_output([str(go), "version"], env=env, text=True).strip()
    go_env = json.loads(subprocess.check_output(
        [str(go), "env", "-json", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOPATH", "GOCACHE", "GOTOOLCHAIN", "GOWORK", "GOFLAGS"],
        env=env, text=True,
    ))
    manifest = {
        "repo": str(repo),
        "package": package_target,
        "trimpath": not args.no_trimpath,
        "harness_ref": args.test_ref,
        "harness_revision": harness,
        "harness_tree": tree(harness),
        "go": str(go),
        "go_version": go_version,
        "go_env": go_env,
        "source_files": source_files,
        "common_test_sha256": test_hashes,
        "excluded_package_tests": extra_tests,
        "variants": {},
    }
    for name, revision in revisions.items():
        directory = output / ("variant" + name)
        directory.mkdir(exist_ok=True)
        files = revision_files(revision)
        replacements = dict(common_replacements)
        hashes = {}
        for filename in [*source_files, *versioned_tests]:
            if filename not in files:
                replacements[str(repo / filename)] = ""
                hashes[filename] = None
                continue
            data = git("show", revision + ":" + filename)
            destination = directory / filename
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
            replacements[str(repo / filename)] = str(destination)
            hashes[filename] = sha256(data)
        overlay = output / ("variant" + name + ".overlay.json")
        binary = output / ("variant" + name + ".test")
        log = output / ("variant" + name + ".build.log")
        save_json(overlay, {"Replace": replacements})
        command = [str(go), "test", "-c", "-tags=embed", "-p=2", "-buildvcs=false"]
        if not args.no_trimpath:
            command.append("-trimpath")
        command += ["-overlay=" + str(overlay), "-o", str(binary), package_target]
        item = {
            "ref": REFS[name], "revision": revision, "tree": tree(revision),
            "overlay": str(overlay), "binary": str(binary), "build_log": str(log),
            "overlay_sha256": sha256(overlay.read_bytes()),
            "source_and_versioned_test_sha256": hashes,
            "excluded_package_tests": extra_tests,
            "command": command, "status": "prepared",
        }
        manifest["variants"][name] = item
        print(f"Prepared {name}: {revision} tree={item['tree']} overlay={overlay}", flush=True)
    manifest_path = output / "build-variants.json"
    save_json(manifest_path, manifest)
    if not args.build:
        print(f"No builds requested. Manifest: {manifest_path}", flush=True)
        return

    for name, item in manifest["variants"].items():
        # Abort on newly changed production inputs. New tests can simply be
        # excluded: the common test set is explicitly fixed at harness_ref.
        current = names("diff", "--name-only", "-z", harness, "--", "tsc")
        current |= names("ls-files", "--others", "--exclude-standard", "-z", "--", "tsc")
        unprotected = sorted(filename for filename in current if production(filename) and filename not in source_files)
        current_tests = {str(path.relative_to(repo)) for path in (repo / package_dir).glob("*_test.go")}
        if unprotected:
            raise RuntimeError("New worktree inputs need a fresh overlay: " + ", ".join(unprotected))
        new_tests = sorted(current_tests - set(test_files) - set(item["excluded_package_tests"]))
        if new_tests:
            overlay = Path(item["overlay"])
            replacements = json.loads(overlay.read_text())
            for filename in new_tests:
                replacements["Replace"][str(repo / filename)] = ""
            save_json(overlay, replacements)
            item["overlay_sha256"] = sha256(overlay.read_bytes())
            item["excluded_package_tests"] = sorted(set(item["excluded_package_tests"]) | set(new_tests))
            save_json(manifest_path, manifest)
        non_go_inputs_match(item["revision"])
        print(f"Building {name} serially: {shlex.join(item['command'])}", flush=True)
        started = time.monotonic()
        with Path(item["build_log"]).open("w") as log:
            log.write(json.dumps({"revision": item["revision"], "tree": item["tree"], "overlay": item["overlay"], "go_version": go_version, "go_env": go_env}) + "\n")
            log.flush()
            result = subprocess.run(item["command"], cwd=repo / "tsc", env=env, stdout=log, stderr=subprocess.STDOUT)
        item["build_seconds"] = round(time.monotonic() - started, 3)
        item["returncode"] = result.returncode
        item["status"] = "built" if result.returncode == 0 else "failed"
        if result.returncode == 0:
            item["binary_sha256"] = sha256(Path(item["binary"]).read_bytes())
        save_json(manifest_path, manifest)
        if result.returncode:
            print(Path(item["build_log"]).read_text(), file=sys.stderr)
            raise SystemExit(result.returncode)
        print(f"Built {name} in {item['build_seconds']}s: {item['binary']}", flush=True)


if __name__ == "__main__":
    main()
