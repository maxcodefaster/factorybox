#!/usr/bin/env python3
"""Prove delivered bounded-worker startup refusals preserve the exact owned input."""
import argparse
import hashlib
import json
import shutil
import subprocess
import tarfile
import threading
import time
import uuid

import build_worker
import consumer_worker_loss
import native_base
import reference
import reference_lifecycle
import upstream_bats


def image_digest(identity, path, expected_bytes, *, timeout=30):
    """Hash Docker's read-only archive stream without expanding the image on disk."""
    command = [*reference.DOCKER, 'cp', identity + ':' + path, '-']
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    timer = threading.Timer(timeout, process.kill)
    timer.daemon = True
    timer.start()
    digest, count = hashlib.sha256(), 0
    try:
        with tarfile.open(fileobj=process.stdout, mode='r|') as archive:
            members = 0
            for entry in archive:
                members += 1
                if members != 1 or not entry.isfile() or entry.size != expected_bytes or entry.name != path.rsplit('/', 1)[1]:
                    raise ValueError('Docker returned an unexpected image archive member')
                with archive.extractfile(entry) as stream:
                    while data := stream.read(1 << 20):
                        count += len(data)
                        if count > expected_bytes:
                            raise ValueError('image exceeds its exact fixed size')
                        digest.update(data)
        trailing = process.stdout.read(1 << 20)
        if len(trailing) == 1 << 20 or count != expected_bytes or process.wait(timeout=10):
            raise ValueError('Docker image stream incomplete or excessive')
        return {'bytes': count, 'sha256': digest.hexdigest()}
    finally:
        timer.cancel()
        if process.poll() is None:
            process.kill()
        process.wait()
        process.stdout.close()
        process.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worker-build', required=True)
    args = parser.parse_args()
    _, worker = build_worker.load(args.worker_build)
    native_base.require_native_host('linux/amd64', json.loads(reference.docker(['info', '--format', '{{json .}}'])))
    if reference.docker(['ps', '--filter', 'label=com.factorybox.run', '--format', '{{.ID}}']):
        raise ValueError('other Factorybox runs must finish before this serialized test')
    owner = uuid.uuid4().hex
    name = 'factorybox-bounded-identity-' + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / 'runner.py')
    result = {'schema': 1, 'owner': owner, 'passed': False, 'cleanup': False, 'production_qualified': False,
              'worker_build': args.worker_build, 'worker_image_id': worker['image_id'],
              'runner_sha256': reference.sha(artifact / 'runner.py'), 'workers': [], 'volumes': [],
              'global_sysctls_before': consumer_worker_loss.sysctls(),
              'scope': 'Exact delivered worker duplicate lease, changed owner, changed capacity and interrupted initialization refusals. '
                       'Full read-only Docker archive streams hash the exact images without extraction; valid reopen preserves a private canary. '
                       'No adoption, reformatting, host installation or production qualification.'}
    reference_lifecycle.write_json(artifact / 'intent.json', result)
    preserve = False
    started = time.monotonic()
    deadline = started + 420
    print(json.dumps({'running': str(artifact)}), flush=True)
    try:
        def volume(suffix):
            value = name + suffix
            result['volumes'].append(value)
            reference.docker(['volume', 'create', '--label', 'com.factorybox.run=' + owner, value])
            return value

        def create(suffix, store, store_owner=owner, size=512 << 20, helper=None):
            row = {'name': name + '-' + suffix, 'removed': False}
            result['workers'].append(row)
            command = ['run', '--detach', '--pull=never', '--name', row['name'], '--label', 'com.factorybox.run=' + owner,
                       '--privileged', '--cgroupns=private', '--cpus=3', '--memory=3g', '--pids-limit=128', '--network=none',
                       '--env', 'GOMAXPROCS=2', '--env', 'GOMEMLIMIT=256MiB',
                       '--mount', f'type=volume,source={store},target=/var/lib/sysbox']
            if helper is not None:
                command += ['--read-only', '--entrypoint', '/usr/bin/python3', worker['image'], '-c', helper]
            else:
                command += [worker['image'], '--store-id', store_owner, '--bounded-root', '/var/lib/sysbox/fixed-store', '--bounded-bytes', str(size)]
            row['command'] = command
            row['id'] = reference.docker(command)
            consumer_worker_loss.exact_worker(row['name'], owner, row['id'])
            return row

        def wait(row, expected_exit):
            reference.docker(['wait', row['id']], timeout=max(.1, min(15, deadline-time.monotonic())))
            row['state'] = consumer_worker_loss.exact_worker(row['name'], owner, row['id'])['State']
            if row['state']['Running'] or row['state']['OOMKilled'] or row['state']['ExitCode'] != expected_exit:
                raise ValueError('owned native fixture exited unexpectedly: ' + row['name'])
            logs = subprocess.run([*reference.DOCKER, 'logs', row['id']], capture_output=True, text=True, timeout=5)
            text = logs.stdout+logs.stderr
            (artifact / (row['name'].removeprefix(name + '-') + '.log')).write_text(text)
            return text

        def remove(row):
            consumer_worker_loss.exact_worker(row['name'], owner, row['id'])
            reference.owned_delete('container', row['name'], owner)
            row['removed'] = True

        store = volume('')
        original = create('original', store)
        status = consumer_worker_loss.ready(original['name'], owner, original['id'], deadline)
        result['startup'] = status
        canary = 'bounded-owned-canary-' + owner
        code = 'from pathlib import Path; p=Path("/var/lib/sysbox/fixed-store/data/store/canary");p.write_text(' + repr(canary) + ')'
        reference.docker(['exec', original['id'], '/usr/bin/python3', '-c', code])
        duplicate = create('duplicate', store)
        if 'bounded store already has a worker' not in wait(duplicate, 1):
            raise ValueError('duplicate worker did not refuse its outer store lease')
        remove(duplicate)
        current = consumer_worker_loss.ready(original['name'], owner, original['id'], deadline)
        if current['status'] != status['status']:
            raise ValueError('duplicate refusal changed the original live worker')
        reference.docker(['stop', '--time', '10', original['id']])
        wait(original, 0)
        path = '/var/lib/sysbox/fixed-store/store.img'
        before = image_digest(original['id'], path, 512 << 20, timeout=max(.1, min(30, deadline-time.monotonic())))
        result['image_before_refusals'] = before
        for suffix, changed_owner, changed_size in [('wrong-owner', uuid.uuid4().hex, 512 << 20),
                                                    ('wrong-size', owner, (512 << 20) + 4096)]:
            rejected = create(suffix, store, changed_owner, changed_size)
            if 'bounded store owner, capacity or saved image identity changed' not in wait(rejected, 1):
                raise ValueError('changed bounded store identity was not refused')
            observed = image_digest(rejected['id'], path, 512 << 20, timeout=max(.1, min(30, deadline-time.monotonic())))
            if observed != before:
                raise ValueError('refused startup changed the exact existing image bytes')
            result[suffix + '_image'] = observed
            remove(rejected)
        remove(original)
        reopened = create('reopened', store)
        reopened_status = consumer_worker_loss.ready(reopened['name'], owner, reopened['id'], deadline)
        if (reopened_status['status']['lifecycle']['bounded_store']['image'] != status['status']['lifecycle']['bounded_store']['image'] or
                reopened_status['status']['epoch'] == status['status']['epoch']):
            raise ValueError('valid reopen changed image identity or reused epoch')
        observed = reference.docker(['exec', reopened['id'], '/usr/bin/python3', '-c',
                                    'from pathlib import Path; print(Path("/var/lib/sysbox/fixed-store/data/store/canary").read_text())'])
        if observed != canary:
            raise ValueError('refusals or reopen changed private persistent canary')
        reference.docker(['stop', '--time', '10', reopened['id']])
        wait(reopened, 0)
        remove(reopened)
        partial = volume('-partial')
        payload = ('uncertain-owned-image-' + owner).encode()
        helper = ('import os;from pathlib import Path; p=Path("/var/lib/sysbox/fixed-store"); p.mkdir(mode=0o700);'
                  'f=os.open(p/"store.img",os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600);os.write(f,' + repr(payload) + ');os.fsync(f);os.close(f)')
        initializer = create('partial-input', partial, helper=helper)
        wait(initializer, 0)
        remove(initializer)
        uncertain = create('partial-refusal', partial)
        if 'refusing unidentified or interrupted bounded-store initialization' not in wait(uncertain, 1):
            raise ValueError('unknown initialization was adopted or reformatted')
        preserved = image_digest(uncertain['id'], path, len(payload), timeout=max(.1, min(30, deadline-time.monotonic())))
        if preserved != {'bytes': len(payload), 'sha256': hashlib.sha256(payload).hexdigest()}:
            raise ValueError('uncertain image bytes changed')
        result['uncertain_image_preserved'] = preserved
        remove(uncertain)
        result.update(passed=True, duplicate_lease_refused=True, changed_owner_refused=True,
                      changed_capacity_refused=True, exact_existing_image_unchanged=True,
                      valid_reopen_canary_preserved=True, interrupted_initialization_refused=True)
    except Exception as error:
        result['error'] = str(error)
    finally:
        try:
            for row in result['workers']:
                if not row['removed']:
                    if row.get('id'):
                        consumer_worker_loss.exact_worker(row['name'], owner, row['id'])
                    reference.owned_delete('container', row['name'], owner)
                    row['removed'] = True
            if not result['passed'] and result['volumes']:
                try:
                    upstream_bats.archive_stopped_volumes(artifact, name, owner, result['volumes'], worker['image'], result,
                                                          timeout=120, compressed=True)
                except Exception as error:
                    preserve = True
                    result['archive_error'] = str(error)
            if not preserve:
                for store in result['volumes']:
                    reference.owned_delete('volume', store, owner)
            result['cleanup'] = not preserve
        finally:
            result['global_sysctls_after'] = consumer_worker_loss.sysctls()
            result['inputs_unchanged'] = reference.sha(artifact / 'runner.py') == result['runner_sha256']
            result['passed'] = (result['passed'] and result['cleanup'] and result['inputs_unchanged'] and
                                result['global_sysctls_after'] == result['global_sysctls_before'])
            result['outer_seconds'] = time.monotonic()-started
            reference_lifecycle.write_json(artifact / 'result.json', result)
            print(json.dumps({'evidence': str(artifact), 'passed': result['passed'], 'cleanup': result['cleanup'],
                              'error': result.get('error')}), flush=True)
    if not result['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
