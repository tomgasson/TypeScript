#!/usr/bin/env python3
"""Freeze exact production variants and a shared benchmark harness.

Variants are LABEL=REF arguments. Package tests come from --test-ref, with
optional --test-override PATH=REF files. --omit-test-function may remove a named
regression test whose API does not exist in an old source revision; benchmark
functions are never removed. Original and final harness hashes are recorded.
Builds occur serially only with --build. No benchmarks are run here.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import time


HERE = Path(__file__).resolve().parent
BASE_REF = "refactor/25-editor-declaration-boundary-benchmarks"


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def save_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def omit_test(text, name):
    if not re.fullmatch(r"Test[A-Za-z0-9_]+", name):
        raise ValueError("Only named regression tests may be omitted: " + name)
    match = re.search(r"(?m)^func " + re.escape(name) + r"\(", text)
    if match is None:
        return text, False
    index = text.index("{", match.end())
    depth, quote, comment = 0, None, None
    while index < len(text):
        char, following = text[index], text[index:index + 2]
        if comment == "line":
            if char == "\n":
                comment = None
        elif comment == "block":
            if following == "*/":
                comment = None
                index += 1
        elif quote:
            if char == "\\" and quote != "`":
                index += 1
            elif char == quote:
                quote = None
        elif following in ("//", "/*"):
            comment = "line" if following == "//" else "block"
            index += 1
        elif char in ('"', "'", "`"):
            quote = char
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[:match.start()] + text[index + 1:], True
        index += 1
    raise ValueError("Unterminated test function: " + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("variants", nargs="+", metavar="LABEL=REF")
    parser.add_argument("--build", action="store_true", help="build prepared variants serially; never runs benchmarks")
    parser.add_argument("--package", choices=["compiler", "parser", "project", "transpile"], required=True)
    parser.add_argument("--no-trimpath", action="store_true", help="required for parser filesystem fixtures; use consistently within a comparison")
    parser.add_argument("--repo", type=Path, default=HERE.parent / "ts-small")
    parser.add_argument("--output-dir", type=Path, default=HERE / "variants")
    parser.add_argument("--go", type=Path, default=HERE.parent / "toolchain/go/bin/go")
    parser.add_argument("--gopath", type=Path, default=Path("/root/go"))
    parser.add_argument("--test-ref", required=True)
    parser.add_argument("--test-override", action="append", default=[], metavar="PATH=REF")
    parser.add_argument("--omit-test-function", action="append", default=[])
    parser.add_argument("--base-ref", default=BASE_REF)
    args = parser.parse_args()
    variant_refs = dict(value.split("=", 1) for value in args.variants)
    if len(variant_refs) != len(args.variants) or any(not re.fullmatch(r"[A-Za-z0-9_-]+", name) for name in variant_refs):
        parser.error("variant labels must be distinct alphanumeric names")
    package_dir = "tsc/internal/" + args.package + "/"
    package_target = "./internal/" + args.package
    repo, output, go = args.repo.resolve(), args.output_dir.resolve(), args.go.resolve()
    output.mkdir(parents=True, exist_ok=True)

    def git(*arguments):
        return subprocess.check_output(["/usr/bin/git", "-C", str(repo), *arguments])

    def resolve(ref):
        return git("rev-parse", "--verify", ref + "^{commit}").decode().strip()

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
    revisions = {name: resolve(ref) for name, ref in variant_refs.items()}
    base = resolve(args.base_ref)
    harness_files = revision_files(harness)
    test_refs = {name: (args.test_ref, harness) for name in harness_files if package_test(name)}
    for override in args.test_override:
        filename, ref = override.split("=", 1)
        if not package_test(filename) or ".." in Path(filename).parts:
            parser.error("test override must be inside the selected package: " + filename)
        test_refs[filename] = (ref, resolve(ref))
    test_files = sorted(test_refs)

    # Include dirty/later source files as well as changes since the base: unrelated
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
    test_sources = {}
    omitted_functions = set()
    for name in test_files:
        test_ref, test_revision = test_refs[name]
        original = git("show", test_revision + ":" + name)
        text = original.decode()
        omitted = []
        for function in args.omit_test_function:
            text, found = omit_test(text, function)
            if found:
                omitted.append(function)
                omitted_functions.add(function)
        data = text.encode()
        destination = common / Path(name).name
        destination.write_bytes(data)
        common_replacements[str(repo / name)] = str(destination)
        test_hashes[name] = sha256(data)
        test_sources[name] = {
            "ref": test_ref, "revision": test_revision, "tree": tree(test_revision),
            "original_sha256": sha256(original), "sha256": sha256(data),
            "omitted_functions": omitted,
        }
    if omitted_functions != set(args.omit_test_function):
        raise ValueError("Requested regression test was not found: " + ", ".join(set(args.omit_test_function) - omitted_functions))
    extra_tests = sorted(
        str(path.relative_to(repo))
        for path in (repo / package_dir).glob("*_test.go")
        if str(path.relative_to(repo)) not in test_files
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
        "GOPATH": str(args.gopath.resolve()),
        "GOTOOLCHAIN": "local",
        "GOWORK": "off",
        "GOFLAGS": "",
        "GOMAXPROCS": "2",
    })
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
        "common_test_sources": test_sources,
        "excluded_package_tests": extra_tests,
        "variants": {},
    }
    for name, revision in revisions.items():
        directory = output / ("variant" + name)
        directory.mkdir(exist_ok=True)
        files = revision_files(revision)
        replacements = dict(common_replacements)
        hashes = {}
        for filename in source_files:
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
            "ref": variant_refs[name], "revision": revision, "tree": tree(revision),
            "overlay": str(overlay), "binary": str(binary), "build_log": str(log),
            "overlay_sha256": sha256(overlay.read_bytes()),
            "source_sha256": hashes,
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
