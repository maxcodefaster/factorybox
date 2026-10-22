import argparse
import unittest

import build_config


class BuildConfigurationTests(unittest.TestCase):
    def test_reference_context_does_not_fall_back_to_the_host(self):
        self.assertEqual(build_config.docker_command({}),
                         ['docker', '--context', 'orbstack'])
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
            {'FACTORYBOX_DOCKER_ELEVATION': 'sh -c'},
        ]:
            with self.subTest(environment=environment), self.assertRaises(ValueError):
                build_config.docker_command(environment)

    def test_explicit_target_network_and_bounds_are_carried_to_buildkit(self):
        parser = argparse.ArgumentParser()
        build_config.add_arguments(parser)
        self.assertEqual(build_config.arguments(parser.parse_args([])),
                         ['--platform=linux/arm64'])
        options = parser.parse_args(['--platform=linux/amd64', '--network=host', '--bounded-build'])
        self.assertEqual(build_config.arguments(options), [
            '--platform=linux/amd64', '--network=host', '--allow=network.host',
            '--resource=memory=3g', '--resource=cpu-period=100000', '--resource=cpu-quota=300000'])
        self.assertEqual(build_config.architecture(options.platform), 'amd64')
        with self.assertRaises(ValueError):
            build_config.architecture('linux/arm/v7')
