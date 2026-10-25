import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import build_worker
import factory_consumer_native as consumer
import reference
import upstream_unit


class NativeFactoryConsumerInputTests(unittest.TestCase):
    def test_timed_out_container_create_retains_exact_cleanup_intent(self):
        containers = []
        command = ['run', '--name', 'owned-directory', '--label',
                   'com.factorybox.run=owned', '--entrypoint', 'mkdir', 'exact-image']
        def timeout(arguments, **options):
            self.assertEqual(arguments, command)
            self.assertEqual(containers, ['owned-directory'])
            self.assertEqual(options, {'timeout': 30})
            raise subprocess.TimeoutExpired(arguments, 30)
        with self.assertRaises(subprocess.TimeoutExpired):
            consumer.run_owned_container(timeout, containers, 'owned-directory',
                                         command[3:], timeout=30)
        self.assertEqual(containers, ['owned-directory'])

    def test_successful_container_create_registers_once_and_returns_identity(self):
        containers = []
        def run(arguments, **options):
            self.assertEqual(arguments, ['run', '--name', 'owned-hub', '--detach', 'exact-image'])
            self.assertEqual(containers, ['owned-hub'])
            return 'exact-container-id\n'
        self.assertEqual(consumer.run_owned_container(
            run, containers, 'owned-hub', ['--detach', 'exact-image']), 'exact-container-id\n')
        self.assertEqual(containers, ['owned-hub'])

    def test_native_candidate_admission_is_explicit_architecture_and_kernel_bound(self):
        for platform, machine in [('linux/amd64', 'x86_64'), ('linux/arm64', 'aarch64')]:
            host = {'OSType': 'linux', 'Architecture': machine, 'KernelVersion': 'owned-kernel'}
            ordinary = {'platform': platform}
            consumer.require_native_worker(ordinary, host, False)
            candidate = {**ordinary, 'native_candidate': build_worker.native_candidate_profile(platform, host)}
            consumer.require_native_worker(candidate, host, True)
            with self.assertRaisesRegex(ValueError, 'explicit'):
                consumer.require_native_worker(candidate, host, False)
            with self.assertRaisesRegex(ValueError, 'kernel'):
                consumer.require_native_worker(candidate, {**host, 'KernelVersion': 'different'}, True)
            with self.assertRaisesRegex(ValueError, 'matching Linux'):
                consumer.require_native_worker(ordinary, {**host, 'Architecture': 'foreign'}, False)

    def test_consumer_build_requires_frozen_current_sources_and_both_native_linux_image_roles(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            name = 'factorybox-build-consumer-' + 'a' * 32
            build = root / 'artifacts' / name
            context = build / 'context'
            context.mkdir(parents=True)
            (context / 'exact.go').write_text('exact consumer source\n')
            runtime = 'owned-runtime@sha256:' + '1' * 64
            hub = 'owned-hub@sha256:' + '2' * 64
            result = {'passed': True, 'cleanup': True, 'platform': 'linux/arm64',
                      'image': runtime, 'hub_image': hub, 'hub_bun': '/opt/factory/opencode/musl/bun',
                      'source_context': upstream_unit.context_manifest(context),
                      'images': {'runtime': {'platform': 'linux/arm64', 'image': runtime},
                                 'hub': {'platform': 'linux/arm64', 'image': hub}}}
            path = build / 'result.json'
            path.write_text(json.dumps(result))
            with patch.object(reference, 'ROOT', root), \
                 patch.object(reference, 'docker', return_value='[{}]'), \
                 patch.object(consumer.native_base, 'verify_image') as verify:
                self.assertEqual(consumer.load_consumer(name, 'linux/arm64')[1], result)
                self.assertEqual(verify.call_count, 2)
                with self.assertRaisesRegex(ValueError, 'current native'):
                    consumer.load_consumer(name, 'linux/amd64')
                (context / 'exact.go').write_text('changed after image build')
                with self.assertRaisesRegex(ValueError, 'current native'):
                    consumer.load_consumer(name, 'linux/arm64')
                with self.assertRaisesRegex(ValueError, 'exact current'):
                    consumer.load_consumer('../redirected', 'linux/arm64')
