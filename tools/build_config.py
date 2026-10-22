"""Explicit build targets and Docker access; never runtime qualification."""
import os
import re

PLATFORMS = ('linux/arm64', 'linux/amd64')


def docker_command(environ=None):
    environ = os.environ if environ is None else environ
    context = environ.get('FACTORYBOX_DOCKER_CONTEXT', 'orbstack')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', context):
        raise ValueError('expected one explicit Docker context name')
    elevation = environ.get('FACTORYBOX_DOCKER_ELEVATION', '')
    prefixes = {'': [], 'sudo': ['sudo', '-n'], 'pkexec': ['pkexec']}
    if elevation not in prefixes:
        raise ValueError('Docker elevation must be empty, sudo or pkexec')
    return [*prefixes[elevation], 'docker', '--context', context]


def architecture(platform):
    if platform not in PLATFORMS:
        raise ValueError('unsupported build platform: ' + str(platform))
    return platform.split('/')[1]


def add_arguments(parser):
    parser.add_argument('--platform', choices=PLATFORMS, default='linux/arm64',
                        help='Compilation target only; does not admit a runtime platform')
    parser.add_argument('--network', choices=['default', 'host'], default='default',
                        help='Build dependency network; host avoids changing IP forwarding')
    parser.add_argument('--bounded-build', action='store_true',
                        help='Require BuildKit RUN limits of 3 CPU and 3 GiB (recent Buildx)')


def arguments(options):
    architecture(options.platform)
    result = ['--platform=' + options.platform]
    if options.network == 'host':
        result += ['--network=host', '--allow=network.host']
    if options.bounded_build:
        result += ['--resource=memory=3g', '--resource=cpu-period=100000',
                   '--resource=cpu-quota=300000']
    return result
