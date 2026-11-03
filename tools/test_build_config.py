import argparse
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import json

import build_config
from test_build_worker import elf


class BuildConfigurationTests(unittest.TestCase):
    def test_collected_name_resolves_only_the_same_local_manifest_and_config(self):
        digest, config = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
        original, published = 'owned-build@' + digest, 'registry.owned/worker@' + digest
        image = {'Id': config, 'RepoDigests': [published, 'foreign@sha256:' + 'c' * 64]}
        calls = []

        def docker(arguments):
            calls.append(arguments)
            if arguments[-1] == original:
                raise build_config.subprocess.CalledProcessError(
                    1, arguments, stderr='Error response from daemon: No such image: ' + original)
            self.assertEqual(arguments, ['image', 'inspect', config])
            return json.dumps([image])

        self.assertEqual(build_config.local_pinned_image(original, config, docker), (published, image))
        self.assertEqual(calls, [['image', 'inspect', original], ['image', 'inspect', config]])
        for changed in [{'Id': config, 'RepoDigests': ['foreign@sha256:' + 'c' * 64]},
                        {'Id': 'sha256:' + 'd' * 64, 'RepoDigests': [published]}]:
            image = changed
            with self.subTest(image=image), self.assertRaisesRegex(ValueError, 'manifest|identity'):
                build_config.local_pinned_image(original, config, docker)

    def test_local_image_errors_do_not_authorize_another_lookup_or_a_pull(self):
        reference = 'owned@sha256:' + 'a' * 64
        for message in ['permission denied', 'Cannot connect to the Docker daemon',
                        'Error response from daemon: No such image: another-image']:
            with self.subTest(message=message):
                docker = Mock(side_effect=build_config.subprocess.CalledProcessError(
                    1, ['image', 'inspect', reference], stderr=message))
                with self.assertRaises(build_config.subprocess.CalledProcessError):
                    build_config.local_pinned_image(reference, 'sha256:' + 'b' * 64, docker)
                docker.assert_called_once_with(['image', 'inspect', reference])

    def test_existing_pinned_name_remains_unchanged(self):
        reference, config = 'owned@sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
        image = {'Id': config, 'RepoDigests': [reference]}
        calls = []
        def docker(arguments):
            calls.append(arguments)
            return json.dumps([image])
        self.assertEqual(build_config.local_pinned_image(reference, config, docker), (reference, image))
        self.assertEqual(calls, [['image', 'inspect', reference]])

    def test_component_platforms_must_be_explicit_and_agree(self):
        for platform in build_config.PLATFORMS:
            self.assertEqual(build_config.require_platform(platform, {"platform": platform}),
                             platform.split("/")[1])
            for other in [{}, {"platform": "darwin/arm64"},
                          {"platform": next(p for p in build_config.PLATFORMS if p != platform)}]:
                with self.assertRaisesRegex(ValueError, "different target platforms"):
                    build_config.require_platform(platform, {"platform": platform}, other)

    def test_delivered_elf_headers_match_the_target(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "executable"
            for platform in build_config.PLATFORMS:
                path.write_bytes(elf(build_config.architecture(platform)))
                build_config.require_elf(path, platform)
                other = next(p for p in build_config.PLATFORMS if p != platform)
                with self.assertRaisesRegex(ValueError, "executable does not match"):
                    build_config.require_elf(path, other)
            for invalid in [b"", b"script", b"\x7fELF\x01\x01\x01" + bytes(57),
                            b"\x7fELF\x02\x02\x01" + bytes(57)]:
                path.write_bytes(invalid)
                with self.assertRaisesRegex(ValueError, "executable does not match"):
                    build_config.require_elf(path, "linux/arm64")

    def test_reference_context_does_not_fall_back_to_the_host(self):
        for system, expected in [('darwin', 'orbstack'), ('linux', 'default')]:
            with patch.object(build_config.sys, 'platform', system):
                self.assertEqual(build_config.docker_command({}),
                                 ['docker', '--context', expected])
        self.assertEqual(build_config.docker_command({'DOCKER_CONTEXT': 'chosen-linux'}),
                         ['docker', '--context', 'chosen-linux'])
        self.assertEqual(build_config.docker_command({
            'DOCKER_CONTEXT': 'ignored', 'FACTORYBOX_DOCKER_CONTEXT': 'owned'}),
            ['docker', '--context', 'owned'])
        self.assertEqual(build_config.docker_command({
            'FACTORYBOX_DOCKER_CONTEXT': 'default',
            'FACTORYBOX_DOCKER_ELEVATION': 'pkexec'}),
            ['pkexec', 'docker', '--context', 'default'])
        self.assertEqual(build_config.docker_command({
            'FACTORYBOX_DOCKER_CONTEXT': 'default',
            'FACTORYBOX_DOCKER_ELEVATION': 'sudo'}),
            ['sudo', '-n', 'docker', '--context', 'default'])

    def test_docker_access_cannot_inject_extra_commands_or_switches(self):
        for environment in [
            {'FACTORYBOX_DOCKER_CONTEXT': '--host=tcp://foreign'},
            {'FACTORYBOX_DOCKER_CONTEXT': 'default --host=foreign'},
            {'FACTORYBOX_DOCKER_CONTEXT': ''},
            {'DOCKER_CONTEXT': '--host=tcp://foreign'},
            {'FACTORYBOX_DOCKER_ELEVATION': 'sh -c'},
        ]:
            with self.subTest(environment=environment), self.assertRaises(ValueError):
                build_config.docker_command(environment)

    def test_explicit_target_network_and_bounds_are_carried_to_buildkit(self):
        parser = argparse.ArgumentParser()
        build_config.add_arguments(parser)
        self.assertEqual(build_config.arguments(parser.parse_args([])),
                         ['--platform=linux/arm64'])
        options = parser.parse_args(['--platform=linux/amd64', '--network=host', '--bounded-build',
                                     '--builder=owned-builder'])
        self.assertEqual(build_config.arguments(options), [
            '--platform=linux/amd64', '--network=host', '--allow=network.host',
            '--builder=owned-builder'])
        self.assertEqual(build_config.architecture(options.platform), 'amd64')
        with self.assertRaises(ValueError):
            build_config.architecture('linux/arm/v7')

    def test_bounded_build_checks_the_actual_single_builder_container(self):
        parser = argparse.ArgumentParser()
        build_config.add_arguments(parser)
        options = parser.parse_args(['--bounded-build', '--builder=owned-builder'])
        backend = {'Name': 'owned-builder', 'Driver': 'docker-container',
                   'Nodes': [{'Name': 'owned-builder0', 'Endpoint': 'owned-context'}]}
        docker = ['docker', '--context', 'owned-context']
        container = {'Id': 'exact-container', 'Image': 'exact-image', 'State': {'Running': True},
                     'HostConfig': {'Memory': 3 * 1024**3, 'CpuPeriod': 100000, 'CpuQuota': 300000}}
        def inspect(*args, **kwargs):
            return json.dumps([container] if 'inspect' in args[0] else backend)
        with patch.object(build_config.subprocess, 'check_output', side_effect=inspect):
            self.assertEqual(build_config.verify_builder(options, docker)['container_id'],
                             'exact-container')
            for key, value in [('Memory', 0), ('Memory', 4 * 1024**3), ('CpuPeriod', 0),
                               ('CpuQuota', -1), ('CpuQuota', 400000)]:
                before = container['HostConfig'][key]
                container['HostConfig'][key] = value
                with self.subTest(key=key, value=value), self.assertRaisesRegex(ValueError, 'ceiling'):
                    build_config.verify_builder(options, docker)
                container['HostConfig'][key] = before
            backend['Nodes'][0]['Endpoint'] = 'another-context'
            with self.assertRaisesRegex(ValueError, 'one explicit'):
                build_config.verify_builder(options, docker)
            backend['Nodes'][0]['Endpoint'] = 'owned-context'
            backend['Nodes'].append({'Name': 'foreign0'})
            with self.assertRaisesRegex(ValueError, 'one explicit'):
                build_config.verify_builder(options, docker)
        for argv in [['--bounded-build'], ['--builder=--foreign']]:
            options = parser.parse_args(argv)
            with self.assertRaises(ValueError):
                build_config.arguments(options)
            with self.assertRaises(ValueError):
                build_config.verify_builder(options, docker)

    def test_default_context_endpoint_uri_must_resolve_to_the_same_daemon(self):
        options = argparse.Namespace(builder='owned-builder', bounded_build=True)
        docker = ['sudo', '-n', 'docker', '--context', 'default']
        backend = {'Name': 'owned-builder', 'Driver': 'docker-container',
                   'Nodes': [{'Name': 'owned-builder0', 'Endpoint': 'unix:///var/run/docker.sock'}]}
        container = {'Id': 'exact-container', 'Image': 'exact-image', 'State': {'Running': True},
                     'HostConfig': {'Memory': 3 * 1024**3, 'CpuPeriod': 100000, 'CpuQuota': 300000}}
        for endpoint in ['unix:///var/run/docker.sock', 'unix:///foreign.sock', 'tcp://foreign:2375']:
            def inspect(command, **kwargs):
                if 'context' in command:
                    self.assertEqual(command, [*docker, 'context', 'inspect', 'default',
                                              '--format', '{{json .Endpoints.docker.Host}}'])
                    return json.dumps(endpoint)
                return json.dumps([container] if 'inspect' in command else backend)
            with self.subTest(endpoint=endpoint), patch.object(build_config.subprocess, 'check_output', side_effect=inspect):
                if endpoint == backend['Nodes'][0]['Endpoint']:
                    self.assertEqual(build_config.verify_builder(options, docker)['container_id'], 'exact-container')
                else:
                    with self.assertRaisesRegex(ValueError, 'one explicit'):
                        build_config.verify_builder(options, docker)
