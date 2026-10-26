#!/usr/bin/env python3
"""Retire a superseded failure dump after retaining metadata, hashes and diagnostics."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import threading
import uuid

import reference
import reference_lifecycle
import upstream_unit


FACTORY_CAPABILITY_ERROR = "shared source requires live worker descriptor retention and its verified CRIU feature"
STANDBY_EXEC_ERROR = "Resumed standby cannot exec a new private Docker container"


def verify_namespace_access_replacement(old, new, old_worker, new_worker, native, proof, old_criu, new_criu):
    identical = ["platform", "native_candidate", "consumer_build", "consumer_image", "hub_image",
                 "hub_bun", "source_sha256", "hub_sha256", "registry_binary_sha256", "registry_source_sha256"]
    error = old.get("error", "")
    if (old.get("passed") is not False or new.get("passed") is not True or
            "/ns/net on procfs: Permission denied" not in error or
            "One or more namespaces doesn't belong to the target user namespace" not in error or
            any(row.get(key) is not True for row in [old, new] for key in
                ["reserve", "cleanup", "inputs_unchanged", "host_sysctls_unchanged", "standby_docker_exec_passed"]) or
            any(row.get("terminal_proof") is not False or row.get("diagnostic_only") is not False for row in [old, new]) or
            any(key not in old or old[key] != new.get(key) for key in identical)):
        raise ValueError("namespace supersession requires the exact resolved Factory capture failure")
    first, after = new.get("before", {}), new.get("after", {})
    if (not first.get("nonce") or after.get("nonce") != first["nonce"] or
            not first.get("pid") or after.get("pid") != first["pid"] or
            after.get("count", 0) <= first.get("count", 0) or
            new.get("child_after", {}).get("RestartCount") != 0 or
            new.get("child_after", {}).get("State", {}).get("Running") is not True):
        raise ValueError("namespace supersession lacks complete app and Docker preservation")
    for key in ["standby_pause", "pause"]:
        paused = new.get(key, {}).get("record", {})
        memory = paused.get("pause", {})
        if (paused.get("phase") != "paused" or not memory.get("completed_at") or
                memory.get("after", {}).get("anon") != 0 or memory.get("snapshot", {}).get("resident_bytes_after") != 0):
            raise ValueError("namespace supersession lacks both memory-releasing pauses")
    core = lambda row: {key: value for key, value in row["source_context"].items() if not key.endswith("_test.go")}
    if (core(old_worker) != core(new_worker) or
            any(old_worker.get(key) != new_worker.get(key) for key in ["platform", "native_candidate"]) or
            any(old_worker["bundle_sha256"][key] != new_worker["bundle_sha256"][key] for key in
                ["factorybox", "components/sysbox-runc", "components/sysbox-fs", "components/sysbox-mgr"]) or
            old_worker["bundle_sha256"]["components/criu"] == new_worker["bundle_sha256"]["components/criu"] or
            new_criu.get("base_candidate") != old_worker.get("criu_build") or
            new_criu.get("candidate_patch_sha256") != {
                **old_criu["candidate_patch_sha256"],
                "namespace-access.patch": reference.sha(reference.ROOT / "upstream/patches/criu-userns-validation-handles.patch")}):
        raise ValueError("namespace supersession is not the isolated ordered CRIU correction")
    cases = proof.get("cases", [])
    protected = [case for case in cases if case.get("protected") is True]
    if (any(native.get(key) is not True for key in ["passed", "cleanup", "inputs_unchanged"]) or
            native.get("suite") != "standby-repeat" or native.get("worker_build") != new.get("worker_build") or
            native.get("platform") != new_worker.get("platform") or
            native.get("worker_image_id") != new_worker.get("image_id") or core(native) != core(new_worker) or
            proof.get("passed") is not True or proof.get("foreign_namespace_refused") is not True or
            [case.get("protected") for case in cases] != [False, True] or
            len(protected) != 1 or protected[0].get("passed") is not True or
            len(protected[0].get("observations", [])) != 2 or
            any(row.get("actor", {}).get("dumpable") != 0 for row in protected[0]["observations"])):
        raise ValueError("namespace supersession lacks its matching protected-process regression")


def verify_factory_consumer_replacement(old, new, old_worker, new_worker):
    identical = ["platform", "native_candidate", "consumer_build", "consumer_image", "hub_image",
                 "hub_bun", "source_sha256", "hub_sha256", "registry_binary_sha256"]
    required = ["cleanup", "inputs_unchanged", "host_sysctls_unchanged"]
    refusal = [row for row in old.get("commands", []) if "pause" in row.get("arguments", [])]
    if (old.get("passed") is not False or new.get("passed") is not True or
            old.get("reserve") is not False or new.get("reserve") is not False or
            old.get("error") != "Native Factory consumer command failed: " + FACTORY_CAPABILITY_ERROR + "\n" or
            old.get("pause") is not None or old.get("resume") is not None or
            len(refusal) != 1 or refusal[0].get("exit") != 1 or
            refusal[0].get("stderr", "").strip() != FACTORY_CAPABILITY_ERROR or
            any(any(row.get(key) is not True for key in required) for row in [old, new]) or
            any(old.get(key) is None or old[key] != new.get(key) for key in identical)):
        raise ValueError("Factory consumer supersession requires the exact closed precapture capability refusal")
    core = lambda row: {key: value for key, value in row["source_context"].items()
                        if not key.endswith("_test.go")}
    components = ["factorybox", "components/sysbox-runc", "components/sysbox-fs", "components/sysbox-mgr"]
    if (core(old_worker) != core(new_worker) or
            any(old_worker.get(key) != new_worker.get(key) for key in ["platform", "native_candidate"]) or
            any(old_worker["bundle_sha256"][key] != new_worker["bundle_sha256"][key] for key in components) or
            old_worker["bundle_sha256"]["components/criu"] == new_worker["bundle_sha256"]["components/criu"]):
        raise ValueError("Factory consumer replacement must preserve runtime/Sysbox and change only CRIU")
    before, after = new.get("before", {}), new.get("after", {})
    pause = new.get("pause", {}).get("record", {})
    memory = pause.get("pause", {})
    child = new.get("child_after", {})
    if (not before.get("nonce") or before.get("nonce") != after.get("nonce") or
            not before.get("pid") or before["pid"] != after.get("pid") or
            type(after.get("count")) is not int or after["count"] <= before.get("count", 0) or
            after.get("editor") != "during-pause" or pause.get("phase") != "paused" or
            not memory.get("completed_at") or memory.get("after", {}).get("anon") != 0 or
            memory.get("snapshot", {}).get("resident_bytes_after") != 0 or
            child.get("State", {}).get("Running") is not True or child.get("RestartCount") != 0):
        raise ValueError("Factory consumer replacement lacks complete live RAM, Source, Docker and released-memory proof")


def verify_standby_exec_replacement(old, new, old_worker, new_worker, native, proof, input_proof=None):
    """Supersede only the resolved exec failure, never the later capture failure."""
    identical = ["platform", "native_candidate", "consumer_build", "consumer_image",
                 "hub_image", "hub_bun", "hub_sha256",
                 "registry_binary_sha256", "registry_source_sha256"]
    required = ["cleanup", "inputs_unchanged", "host_sysctls_unchanged"]
    failed_exec = old.get("standby_docker_exec", {})
    if (old.get("passed") is not False or old.get("error") != STANDBY_EXEC_ERROR or
            any(row.get("reserve") is not True or row.get("terminal_proof") is not False
                for row in [old, new]) or
            any(row.get(key) is not True for row in [old, new] for key in required) or
            any(key not in old or old[key] != new.get(key) for key in identical) or
            failed_exec.get("exit") != 128 or
            "error copying bootstrap data to pipe: write init-p: broken pipe" not in failed_exec.get("stdout", "") or
            new.get("diagnostic_only") is not False or
            new.get("standby_docker_exec_passed") is not True or
            new.get("standby_docker_exec", {}).get("exit") != 0 or
            new.get("standby_docker_exec", {}).get("stdout") != "factorybox-standby-exec" or
            new.get("hub_before", {}).get("connected") is not True or
            not new.get("before", {}).get("nonce") or not new.get("before", {}).get("pid")):
        raise ValueError("standby exec supersession lacks the same real resolved Factory failure")
    if old.get("source_sha256") != new.get("source_sha256"):
        before, after = old.get("source_sha256", {}), new.get("source_sha256", {})
        changed = {key for key in before.keys() | after.keys() if before.get(key) != after.get(key)}
        first, resumed = (input_proof or {}).get("before", {}), (input_proof or {}).get("after", {})
        memory = (input_proof or {}).get("pause", {}).get("record", {}).get("pause", {})
        if (changed != {"prepare.py", "prepare_test.py"} or input_proof is None or
                any(input_proof.get(key) is not True for key in
                    ["passed", "cleanup", "inputs_unchanged", "host_sysctls_unchanged",
                     "sibling_isolation", "preparation_ownership_controls_passed"]) or
                input_proof.get("source_sha256") != after or
                any(input_proof.get(key) != new.get(key) for key in identical) or
                not first.get("nonce") or first.get("nonce") != resumed.get("nonce") or
                not first.get("pid") or first.get("pid") != resumed.get("pid") or
                resumed.get("count", 0) <= first.get("count", 0) or
                not memory.get("completed_at") or memory.get("after", {}).get("anon") != 0 or
                memory.get("snapshot", {}).get("resident_bytes_after") != 0):
            raise ValueError("standby exec input upgrade lacks its separate complete isolation and RAM proof")
    if new.get("passed") is not True:
        error = new.get("error", "")
        if (new.get("passed") is not False or new.get("pause_attempted") is not True or
                "/ns/net on procfs: Permission denied" not in error or
                "One or more namespaces doesn't belong to the target user namespace" not in error):
            raise ValueError("standby exec supersession cannot discard another unresolved failure")
    paused = new.get("standby_pause", {}).get("record", {})
    resumed = new.get("standby_resume", {}).get("record", {})
    started = new.get("start", {}).get("record", {})
    memory = paused.get("pause", {})
    if (paused.get("phase") != "paused" or resumed.get("phase") != "running" or
            any(not started.get(key) or paused.get(key) != started[key] or resumed.get(key) != started[key]
                for key in ["id", "runtime_id"]) or
            not memory.get("completed_at") or memory.get("after", {}).get("anon") != 0 or
            memory.get("snapshot", {}).get("resident_bytes_after") != 0 or
            any(not memory.get(key) or memory[key] != resumed.get("pause", {}).get(key)
                for key in ["snapshot_id", "checkpoint_sha256"])):
        raise ValueError("standby exec replacement lacks its exact RAM-releasing Pause/Resume")
    core = lambda row: {key: value for key, value in row["source_context"].items()
                        if not key.endswith("_test.go")}
    before, after = core(old_worker), core(new_worker)
    changed = {key for key in before.keys() | after.keys() if before.get(key) != after.get(key)}
    if (changed != {"internal/lifecycle/capture_worker_linux.go", "internal/lifecycle/capture_stop_linux.go"} or
            any(old_worker.get(key) != new_worker.get(key) for key in ["platform", "native_candidate"]) or
            any(old_worker["bundle_sha256"][key] != new_worker["bundle_sha256"][key] for key in
                ["components/criu", "components/sysbox-runc", "components/sysbox-fs", "components/sysbox-mgr"])):
        raise ValueError("standby exec replacement is not the isolated frozen-source stop correction")
    if (any(native.get(key) is not True for key in ["passed", "cleanup", "inputs_unchanged"]) or
            native.get("suite") != "standby-exec" or native.get("worker_build") != new.get("worker_build") or
            native.get("platform") != new_worker["platform"] or
            native.get("worker_image_id") != new_worker["image_id"] or
            native.get("nested_exec_input", {}).get("consumer_build") != new["consumer_build"] or
            native.get("nested_exec_input", {}).get("image") != new["consumer_image"] or
            core(native) != after or proof.get("passed") is not True or
            "nested-exec-ready" not in proof.get("before", "") or proof.get("after") != "nested-exec-ready\n"):
        raise ValueError("standby exec replacement lacks the matching passing native regression")


def sync_json(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n"); stream.flush(); os.fsync(stream.fileno())


def verify_bats_replacement(run, proof, old, new):
    if (old.get('suite') != 'systemd' or new.get('suite') != 'systemd' or
            old.get('target_platform') != 'linux/amd64' or new.get('target_platform') != 'linux/amd64' or
            any(type(row.get('upstream_test_files_verified')) is not int or row['upstream_test_files_verified'] <= 0 or
                row['upstream_test_files_verified'] != row['unchanged_upstream_test_files'] + row['declared_upstream_test_changes'] or
                row.get('inputs_unchanged') is not True or
                row.get('components_unchanged') is not True for row in [old, new]) or
            old.get('unchanged_upstream_test_files') != new.get('unchanged_upstream_test_files') or
            old.get('declared_upstream_test_changes') != new.get('declared_upstream_test_changes') or
            not set(old['expected_titles']).issubset(new['expected_titles']) or
            old['inputs_sha256'].get('source.tar') != new['inputs_sha256'].get('source.tar')):
        raise ValueError('Bats supersession requires the same original upstream source and assertions')
    cases = {row['name']: row for row in new['observed']['cases']}
    if (new['observed'].get('complete') is not True or new['observed'].get('failed') or not old['observed']['failed'] or
            any(name not in cases or cases[name].get('passed') is not True or cases[name].get('skipped') is not False
                for name in old['observed']['failed'])):
        raise ValueError('every formerly failed Bats assertion must actually pass without a skip')
    for path, record in [(run, old), (proof, new)]:
        for name, digest in record['inputs_sha256'].items():
            supplied = path / name
            if supplied.resolve() != supplied or not supplied.is_relative_to(path) or reference.sha(supplied) != digest:
                raise ValueError('Bats frozen input changed before retention')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-run", required=True)
    parser.add_argument("--replacement-proof", required=True)
    parser.add_argument("--reason", required=True, help="Concrete supersession and remaining reconstruction limits")
    parser.add_argument("--verify-only",action="store_true",help="Retain and verify diagnostics while keeping the complete source archive")
    parser.add_argument("--resolved-standby-exec", metavar="NATIVE_RUN",
                        help="Retire only an exact resolved standby exec failure, backed by a passing native regression and real Factory first work; retain and verify the later complete capture failure")
    parser.add_argument("--resolved-namespace-access", metavar="NATIVE_RUN",
                        help="Retire only the resolved namespace lookup failure after the complete same-input Factory reserve and protected-process regression pass")
    parser.add_argument("--factory-input-proof", metavar="FACTORY_RUN",
                        help="With resolved standby exec only: a complete passing Factory proof may qualify the isolated prepare.py/prepare_test.py input-ownership upgrade")
    args = parser.parse_args()
    pattern = r"factorybox-(?:devx-lifecycle|consumer-worker-loss|checkpoint-retirement|bats|factory-consumer)-[0-9a-f]{32}"
    if not all(re.fullmatch(pattern, name) for name in [args.source_run, args.replacement_proof]):
        raise ValueError("requires exact native consumer failure/proof IDs")
    run, proof = (reference.ROOT/"artifacts"/name for name in [args.source_run, args.replacement_proof])
    if run.resolve()!=run or proof.resolve()!=proof:
        raise ValueError("native evidence cannot be redirected through symlinks")
    source_result, replacement = (json.loads((path/"result.json").read_text()) for path in [run,proof])
    factory = any(name.startswith('factorybox-factory-consumer-') for name in [args.source_run, args.replacement_proof])
    replacement_scope = None
    if args.resolved_standby_exec and args.resolved_namespace_access:
        raise ValueError("select one exact resolved failure scope")
    if args.resolved_standby_exec and not factory:
        raise ValueError("scoped standby exec supersession requires Factory consumer runs")
    if args.resolved_namespace_access and not factory:
        raise ValueError("scoped namespace supersession requires Factory consumer runs")
    if args.factory_input_proof and not args.resolved_standby_exec:
        raise ValueError("Factory input upgrade proof requires scoped standby exec supersession")
    if factory:
        if not all(name.startswith('factorybox-factory-consumer-') for name in [args.source_run, args.replacement_proof]):
            raise ValueError('Factory consumer failure requires a complete actual Factory consumer replacement')
        import build_worker
        workers = [build_worker.load(row['worker_build']) for row in [source_result, replacement]]
        old_worker, new_worker = [row[1] for row in workers]
        if args.resolved_namespace_access:
            import criu_candidate
            if not re.fullmatch(r"factorybox-checkpoint-retirement-[0-9a-f]{32}", args.resolved_namespace_access):
                raise ValueError("requires one exact namespace regression")
            native_run = reference.ROOT / "artifacts" / args.resolved_namespace_access
            native_path, native_proof = native_run / "result.json", native_run / "output/standby-repeat.json"
            if any(path.resolve() != path for path in [native_path, native_proof]):
                raise ValueError("namespace regression cannot be redirected")
            native = json.loads(native_path.read_text())
            if reference.sha(native_proof) != native.get("proof_sha256"):
                raise ValueError("namespace regression changed")
            components = [criu_candidate.load(row["criu_build"],
                base_lock=path / "criu-base-source-lock.json" if (path / "criu-base-source-lock.json").is_file() else None)[1]
                for path, row in workers]
            verify_namespace_access_replacement(source_result, replacement, old_worker, new_worker,
                native, json.loads(native_proof.read_text()), *components)
            replacement_scope = {"passed": True, "scope": "resolved namespace lookup only",
                                 "whole_consumer_passed": True, "native_result_sha256": reference.sha(native_path),
                                 "native_proof_sha256": reference.sha(native_proof)}
        elif args.resolved_standby_exec:
            if not re.fullmatch(r"factorybox-checkpoint-retirement-[0-9a-f]{32}", args.resolved_standby_exec):
                raise ValueError("requires an exact native standby exec regression")
            native_run = reference.ROOT / "artifacts" / args.resolved_standby_exec
            native_path, native_proof = native_run / "result.json", native_run / "output/standby-exec.json"
            if any(path.resolve() != path for path in [native_path, native_proof]):
                raise ValueError("native standby exec proof cannot be redirected")
            native = json.loads(native_path.read_text())
            if reference.sha(native_proof) != native.get("proof_sha256"):
                raise ValueError("native standby exec proof changed")
            input_run = input_record = None
            if args.factory_input_proof:
                if not re.fullmatch(r"factorybox-factory-consumer-[0-9a-f]{32}", args.factory_input_proof):
                    raise ValueError("requires an exact passing Factory input-isolation proof")
                input_run = reference.ROOT / "artifacts" / args.factory_input_proof
                if input_run.resolve() != input_run or (input_run / "result.json").is_symlink():
                    raise ValueError("Factory input proof cannot be redirected")
                input_record = json.loads((input_run / "result.json").read_text())
                for file, digest in input_record.get("source_sha256", {}).items():
                    if reference.sha(input_run / "factory" / file) != digest:
                        raise ValueError("Factory input proof source changed")
                if reference.sha(input_run / "hub.js") != input_record.get("hub_sha256"):
                    raise ValueError("Factory input proof Hub changed")
            verify_standby_exec_replacement(source_result, replacement, old_worker, new_worker,
                                           native, json.loads(native_proof.read_text()), input_record)
            retained = None
            if replacement.get("passed") is False:
                archive = proof / "failed-volume-0.tar"
                encoding_path = proof / "failed-volume-0.tar.encoding.json"
                if encoding_path.resolve() != encoding_path:
                    raise ValueError("current unresolved capture archive cannot be redirected")
                encoding = json.loads(encoding_path.read_text())
                if replacement.get("failed_volume_archives", {}).get(proof.name) != encoding["original"]:
                    raise ValueError("current unresolved capture lacks its complete exact archive")
                reference.artifact_storage.verify_compacted(archive, encoding["original"])
                retained = {"run": proof.name, "archive": str(archive),
                            "encoding_receipt_sha256": reference.sha(encoding_path),
                            "whole_original_decode_verified": True}
            replacement_scope = {"passed": True, "scope": "resolved standby exec only, not full consumer acceptance",
                                 "whole_consumer_passed": replacement["passed"],
                                 "native_result_sha256": reference.sha(native_path),
                                 "native_proof_sha256": reference.sha(native_proof),
                                 "unresolved_capture_preserved": retained}
            if input_run:
                replacement_scope["separate_input_upgrade_proof"] = {
                    "run": input_run.name, "result_sha256": reference.sha(input_run / "result.json")}
        else:
            import build_criu_shared_fds
            import criu_candidate
            verify_factory_consumer_replacement(source_result, replacement, old_worker, new_worker)
            old_criu, new_criu = [criu_candidate.load(row['criu_build'])[1] for row in [old_worker, new_worker]]
            if (new_criu.get('base_candidate') != old_worker['criu_build'] or
                    'shared-fds.patch' in old_criu.get('candidate_patch_sha256', {}) or
                    new_criu.get('candidate_patch_sha256') != {
                        **old_criu['candidate_patch_sha256'],
                        'shared-fds.patch': reference.sha(build_criu_shared_fds.PATCH)} or
                    any(new_criu.get('features', {}).get(key) is not True for key in
                        ['shared_file_descriptions', 'shared_source_directories'])):
                raise ValueError('Factory consumer supersession requires only the exact existing shared-description delta')
        for path, record in [(run, source_result), (proof, replacement)]:
            if (any(reference.sha(path/'factory'/name) != digest for name, digest in record['source_sha256'].items()) or
                    reference.sha(path/'hub.js') != record['hub_sha256'] or
                    reference.sha(path/'project-images.json') != reference.sha(run/'project-images.json')):
                raise ValueError('Factory consumer frozen bootstrap, Hub or project-image pins changed')
    bats = any(name.startswith('factorybox-bats-') for name in [args.source_run, args.replacement_proof])
    if bats:
        if not all(name.startswith('factorybox-bats-') for name in [args.source_run, args.replacement_proof]):
            raise ValueError('Bats failure requires a complete Bats replacement')
        verify_bats_replacement(run, proof, source_result, replacement)
    if any(name.startswith('factorybox-checkpoint-retirement-') for name in [args.source_run, args.replacement_proof]):
        # Only supersede a fixture that failed on an invalid CLI verb before
        # its metadata assertions. Real runtime failures are not eligible.
        log = (run / 'stdout.txt').read_text()
        if (not all(name.startswith('factorybox-checkpoint-retirement-') for name in [args.source_run, args.replacement_proof]) or
                source_result.get('suite') != 'record-metadata' or replacement.get('suite') != 'record-metadata' or
                not any('unknown lifecycle command "' + verb + '"' in log for verb in ['status', 'inspect']) or
                replacement.get('passed_tests') != 1 or
                '--- PASS: TestNativeRecordMetadataResumeAdmission ' not in (proof / 'stdout.txt').read_text()):
            raise ValueError('checkpoint fixture retirement requires an invalid-verb failure and a passing corrected full metadata test')
        commands = lambda row: {key: value for key, value in row['source_context'].items()
                                if key.startswith('cmd/') and not key.endswith('_test.go')}
        if commands(source_result) != commands(replacement):
            raise ValueError('invalid-verb supersession requires the same public command implementation')
        checked = proof / 'output/record-metadata.json'
        evidence = json.loads(checked.read_text())
        if (reference.sha(checked) != replacement.get('proof_sha256') or
                any(evidence.get(key) is not True for key in ['passed', 'injected_history',
                    'resume_refused_before_transition', 'committed_pause_record_unchanged',
                    'source_still_stopped', 'no_resume_backing', 'prior_checkpoint_independent'])):
            raise ValueError('corrected metadata fixture proof is incomplete or changed')
    if any(path.is_symlink() for path in [run/"result.json",proof/"result.json",run/"failed-volume-0.tar.encoding.json"]):
        raise ValueError("native evidence receipts must not be symlinks")
    if (source_result.get("passed") is not False or source_result.get("cleanup") is not True or
            replacement.get("cleanup") is not True or
            replacement.get("passed") is not True and replacement_scope is None):
        raise ValueError("requires a closed failure and a complete passing replacement proof")
    old = run/"failed-volume-0.tar.zst"
    pointer = old.with_name(old.name+".retired.json")
    if pointer.exists() or pointer.is_symlink():
        raise ValueError("failure payload already has a retirement receipt")
    encoding=json.loads((run/"failed-volume-0.tar.encoding.json").read_text())
    original,encoded=encoding["original"],encoding["encoded"]
    native_originals=[row for row in source_result.get("failed_volume_archives",{}).values() if row.get("file")==original["file"]]
    if native_originals!=[original]:
        raise ValueError("encoding receipt differs from the original native failure result")
    for path,record in ([] if bats or factory else [(run,source_result),(proof,replacement)]):
        context=path/("source" if (path/"source").is_dir() else "context")
        if upstream_unit.context_manifest(context)!=record["source_context"]:
            raise ValueError("frozen native source context changed")
    source=old
    if not source.exists():
        source,encoded=reference.artifact_storage.verified_replacement(source,encoded,original)
    before=reference.artifact_storage.identity(source)
    with source.open("rb") as stream:
        if hashlib.file_digest(stream,"sha256").hexdigest()!=encoded["sha256"]:
            raise ValueError("encoded source differs from its receipt")
    reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    if min(reference.artifact_storage.ARTIFACT_LIMIT-reference.artifact_storage.allocated_bytes(reference.ROOT/"artifacts"),
           shutil.disk_usage(reference.ROOT).free-reference.artifact_storage.FREE_RESERVE) < 128<<20:
        raise ValueError("retirement needs space for bounded metadata and diagnostic retention")
    artifact=reference.ROOT/"artifacts"/("factorybox-storage-retirement-"+uuid.uuid4().hex)
    artifact.mkdir(mode=0o700);(artifact/"payloads").mkdir(mode=0o700)
    shutil.copyfile(__file__,artifact/"runner.py")
    result={"schema":1,"operation":"retire_superseded_failed_volume","source_run":args.source_run,
            "source_result_sha256":reference.sha(run/"result.json"),"replacement_proof":args.replacement_proof,
            "replacement_result_sha256":reference.sha(proof/"result.json"),"source_archive":str(source),
            "source_encoded_sha256":encoded["sha256"],"original":original,"reason":args.reason,
            "source_encoding_receipt_sha256":reference.sha(run/"failed-volume-0.tar.encoding.json"),
            "complete_archive_reconstructable_after_retirement":False,"passed":False,"retired":False}
    if replacement_scope is not None:
        result["replacement_scope"] = replacement_scope
    reference_lifecycle.write_json(artifact/"result.json",result)
    print(json.dumps({"retiring":str(artifact)}),flush=True)
    class Hashed:
        def __init__(self,stream):self.stream,self.hash,self.count=stream,hashlib.sha256(),0
        def read(self,size=-1):
            data=self.stream.read(size);self.hash.update(data);self.count+=len(data)
            if self.count>original["bytes"]:raise ValueError("source exceeds its verified decoded size")
            return data
    members,retained_bytes=0,0
    decoder=None
    def stop_decoder():
        if decoder is not None and decoder.poll() is None:decoder.kill()
    timer=threading.Timer(180,stop_decoder);timer.daemon=True;timer.start()
    try:
        with (artifact/"decode.log").open("xb") as log, gzip.open(artifact/"members.jsonl.gz","xb") as metadata, subprocess.Popen(
                ["zstd","-q","-dc","--long=31","--",str(source)],stdout=subprocess.PIPE,stderr=log) as decoder:
            stream=Hashed(decoder.stdout)
            with tarfile.open(fileobj=stream,mode="r|") as archive:
                for entry in archive:
                    members+=1
                    if members>1_000_000:raise ValueError("source member count exceeds its bound")
                    row={"name":entry.name,"size":entry.size,"type":entry.type.decode("latin1"),"mode":entry.mode,
                         "uid":entry.uid,"gid":entry.gid,"uname":entry.uname,"gname":entry.gname,"mtime":entry.mtime,
                         "linkname":entry.linkname,"devmajor":entry.devmajor,"devminor":entry.devminor,"pax_headers":entry.pax_headers}
                    if entry.isfile():
                        diagnostic=("/captures/" in entry.name and "/filesystem/" not in entry.name and "/pages-" not in entry.name or
                                    entry.name.endswith((".log","/record.json","/config.json")) or
                                    "/bundle/upper/workspace/project/" in entry.name and "/node_modules/" not in entry.name)
                        keep=diagnostic and entry.size<=4<<20
                        digest,parts,count=hashlib.sha256(),[],0
                        with archive.extractfile(entry) as content:
                            while data:=content.read(1<<20):
                                digest.update(data);count+=len(data)
                                if keep:parts.append(data)
                        if count!=entry.size:raise ValueError("incomplete member content")
                        row["sha256"]=digest.hexdigest()
                        if keep:
                            target=artifact/"payloads"/row["sha256"]
                            if not target.exists():
                                retained_bytes+=count
                                if retained_bytes>64<<20:raise ValueError("selected diagnostics exceed their retention bound")
                                with target.open("xb") as output:
                                    output.write(b"".join(parts));output.flush();os.fsync(output.fileno())
                            row["retained_content"]=str(target.relative_to(artifact))
                    metadata.write((json.dumps(row,separators=(",",":"))+"\n").encode())
            while stream.read(1<<20):pass
            if decoder.wait(timeout=30) or stream.count!=original["bytes"] or stream.hash.hexdigest()!=original["sha256"]:
                raise ValueError("complete source decompression differs from its verified original")
        if reference.artifact_storage.identity(source)!=before or reference.sha(run/"result.json")!=result["source_result_sha256"] or reference.sha(proof/"result.json")!=result["replacement_result_sha256"]:
            raise ValueError("closed source or proof changed during retirement")
        for path in [artifact/"members.jsonl.gz",artifact/"runner.py"]:
            with path.open("rb") as retained:os.fsync(retained.fileno())
        verification={**result,"passed":True,"verification_only":True,"complete_original_decompression_verified":True,"members":members,"retained_unique_bytes":retained_bytes,
                      "member_index_sha256":reference.sha(artifact/"members.jsonl.gz"),"reclaimed_encoded_bytes":before[2],
                      "scope":"Every member header and regular-file SHA-256 retained; selected controller/CRIU/workspace diagnostics retain contents. Large superseded payloads are intentionally unavailable. Frozen source, native results/logs and original consumer/build inputs are unchanged."}
        sync_json(artifact/"verification.json",verification)
        if args.verify_only:
            result.update(passed=True,retired=False,verification_only=True,complete_original_decompression_verified=True,members=members,retained_unique_bytes=retained_bytes)
            print(json.dumps({"evidence":str(artifact),"retired":False,"complete_original_decompression_verified":True,"retained_unique_bytes":retained_bytes}),flush=True)
            return
        marker={"schema":1,"source_run":args.source_run,"original":original,"retirement_verification":str(artifact/"verification.json"),
                "verification_sha256":reference.sha(artifact/"verification.json"),"complete_archive_reconstructable":False}
        for path in dict.fromkeys([pointer,source.with_name(source.name+".retired.json")]):sync_json(path,marker)
        for directory in [artifact,artifact/"payloads",run,source.parent]:
            fd=os.open(directory,os.O_RDONLY|os.O_DIRECTORY)
            try:os.fsync(fd)
            finally:os.close(fd)
        source.unlink()
        fd=os.open(source.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(fd)
        finally:os.close(fd)
        result.update(passed=True,retired=True,complete_original_decompression_verified=True,members=members,retained_unique_bytes=retained_bytes)
        print(json.dumps({"evidence":str(artifact),"retired_encoded_bytes":before[2],"retained_unique_bytes":retained_bytes}),flush=True)
    except Exception as error:
        result["error"]=str(error)
        raise
    finally:
        timer.cancel()
        reference_lifecycle.write_json(artifact/"result.json",result)


if __name__=="__main__":main()
