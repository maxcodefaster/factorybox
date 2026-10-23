#!/usr/bin/env python3
"""Observe and interrupt only the exact owned worker after its exit fence."""
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import subprocess
import sys
import time

import reference


def freeze(name, artifact):
    if not re.fullmatch(r'factorybox-drain-watch-inputs-[0-9a-f]{32}', name):
        raise ValueError('expected an exact drain-watch input artifact')
    source = reference.ROOT / 'artifacts' / name
    if source.resolve() != source or any((source / file).is_symlink() for file in
            ['result.json', 'watch.py', 'negative_checks.py', 'negative-result.json']):
        raise ValueError('drain watcher inputs cannot be redirected through symlinks')
    result = json.loads((source / 'result.json').read_text())
    if (result.get('passed') is not True or result.get('cleanup') is not True or
            result.get('worker_drain_qualified') is not False or
            any(reference.sha(source / file) != result[field] for file, field in
                [('watch.py', 'watch_sha256'), ('negative_checks.py', 'negative_checks_sha256'),
                 ('negative-result.json', 'negative_result_sha256')])):
        raise ValueError('drain watcher preflight or frozen bytes differ')
    shutil.copyfile(source / 'watch.py', artifact / 'drain-watch.py')
    shutil.copyfile(__file__, artifact / 'consumer_drain_interrupt.py')
    return {'artifact': name, 'result_sha256': reference.sha(source / 'result.json'),
            'watch_sha256': result['watch_sha256'], 'python': sys.executable,
            'python_sha256': reference.sha(Path(sys.executable)),
            'scope': 'Host observer signals only the exact owned live worker via pidfd after observing its committed exit fence. No runtime hook or assertion change. Same boot process-loss only.'}


def stop(name, owner, identity, worker, status, artifact, phase_artifact, output, deadline, exact_worker):
    saved = exact_worker(name, owner, identity)
    if not saved['State']['Running'] or saved['Image'] != worker['image_id'] or saved['State']['Pid'] <= 1:
        raise ValueError('drain fault requires the exact live delivered worker')
    pid = saved['State']['Pid']
    with open('/proc/' + str(pid) + '/stat') as stream:
        birth = stream.read().rsplit(')', 1)[1].split()[19]
    source = json.loads((output / 'cli-03-pause.json').read_text())['record']
    if source['id'] != 'source' or source['phase'] != 'paused' or not source['pause'].get('completed_at'):
        raise ValueError('drain fault requires the consumer original committed pause')
    request = {'host_pid': pid, 'host_birth': birth, 'worker_status': status,
               'source_record': source, 'cli_sha256': worker['bundle_sha256']['factorybox'], 'deadline': deadline}
    receipt = {'attempted': True, 'passed': False, 'request': request}
    command = [*(['sudo', '-n'] if os.geteuid() else []), sys.executable, '-I',
               str(artifact / 'drain-watch.py'), '--request', json.dumps(request)]
    process, prefix, remainder = None, '', ''
    try:
        with (phase_artifact / 'drain-watch.stderr').open('x') as error:
            process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                       stderr=error, text=True)
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                if not selector.select(min(5, max(0, deadline - time.monotonic()))):
                    raise TimeoutError('owned drain watcher did not arm')
                prefix = process.stdout.readline(65537)
            armed = json.loads(prefix)
            if not armed.get('armed') or armed['host_pid'] != pid or armed['host_birth'] != birth:
                raise ValueError('drain watcher armed on another process')
            receipt['armed'] = armed
            current = exact_worker(name, owner, identity)
            if not current['State']['Running'] or current['State']['Pid'] != pid or current['Image'] != saved['Image']:
                raise ValueError('owned worker changed after watcher armed')
            remaining = deadline - time.monotonic()
            if remaining <= 2:
                raise TimeoutError('drain fault has no remaining lifecycle Stop budget')
            grace = min(45, int(remaining) - 1)
            receipt['grace_seconds'] = grace
            reference.docker(['container', 'stop', '--time=' + str(grace), identity], timeout=remaining)
            remainder, _ = process.communicate(timeout=min(10, max(1, deadline - time.monotonic())))
            outcome = json.loads(remainder)['outcome']
            receipt['outcome'] = outcome
            receipt['watcher_exit'] = process.returncode
            if process.returncode or not all(outcome.get(key) is True for key in
                    ['passed', 'signal_sent', 'handles_closed', 'completion_absent_after_death', 'owned_process_death_confirmed']):
                raise ValueError('selected worker-drain interruption was not fully observed')
            receipt['passed'] = True
    except Exception as error:
        receipt['error'] = str(error)
        raise
    finally:
        if process is not None and process.poll() is None:
            # EOF cancels the watch. Its pidfd always refers to the original
            # worker; a replacement must not start while the watcher is live.
            remainder, _ = process.communicate(timeout=10)
        if process is not None:
            receipt['watcher_exit'] = process.returncode
        (phase_artifact / 'drain-watch.stdout').write_text(prefix + remainder)
        (phase_artifact / 'drain-interruption.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt
