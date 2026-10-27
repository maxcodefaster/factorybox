"""Explicit build targets and Docker access; never runtime qualification."""
import os
import re
import json
import subprocess
import sys

PLATFORMS = ('linux/arm64', 'linux/amd64')


def docker_command(environ=None):
    environ = os.environ if environ is None else environ
    context = environ.get('FACTORYBOX_DOCKER_CONTEXT',
                          environ.get('DOCKER_CONTEXT', 'orbstack' if sys.platform == 'darwin' else 'default'))
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


def require_elf(path, platform):
    """Check delivered ELF bytes, not a builder's declared target."""
    machine = {'amd64': 62, 'arm64': 183}[architecture(platform)]
    with path.open('rb') as stream:
        header = stream.read(20)
    if (len(header) != 20 or header[:7] != b'\x7fELF\x02\x01\x01' or
            int.from_bytes(header[16:18], 'little') not in {2, 3} or
            int.from_bytes(header[18:20], 'little') != machine):
        raise ValueError(f'executable does not match {platform}: {path.name}')


def require_platform(platform, *inputs):
    target = architecture(platform)
    if any(row.get('platform') != platform for row in inputs):
        raise ValueError('worker components have different target platforms')
    return target


def candidate_compile_options(platform, kernel):
    target = architecture(platform)
    if not isinstance(kernel, str) or not re.fullmatch(r"[A-Za-z0-9_.+-]+", kernel):
        raise ValueError("unsupported candidate kernel release encoding")
    return ["-tags=factorybox_candidate_" + target, "-ldflags=-X=" +
            "factorybox/internal/platform.candidateKernel=" + kernel]


def add_arguments(parser, *, bounded_builder=True, required_platform=False):
    parser.add_argument('--platform', choices=PLATFORMS, required=required_platform,
                        default=None if required_platform else 'linux/arm64',
                        help='Compilation target only; does not admit a runtime platform')
    parser.add_argument('--network', choices=['default', 'host'], default='default',
                        help='Build dependency network; host avoids changing IP forwarding')
    if bounded_builder:
        parser.add_argument('--bounded-build', action='store_true',
                            help='Require an explicit single-node docker-container builder limited to 3 CPU and 3 GiB')
        parser.add_argument('--builder', help='Explicit existing Buildx builder; does not change the selected default')
    else:
        parser.set_defaults(bounded_build=False, builder=None)


def verify_builder(options, docker):
    if options.builder is not None and not re.fullmatch(r'[a-z][a-z0-9-]{0,62}', options.builder):
        raise ValueError('expected one explicit Buildx builder name')
    if not options.bounded_build:
        return None
    if not options.builder:
        raise ValueError('--bounded-build requires --builder with verified container limits')
    listing = subprocess.check_output(
        [*docker, 'buildx', 'ls', '--format', '{{json .}}'], text=True, timeout=30)
    matches = [row for line in listing.splitlines() if line.strip()
               if (row := json.loads(line)).get('Name') == options.builder]
    if not matches or any(row != matches[0] for row in matches):
        raise ValueError('bounded build requires one explicit docker-container builder')
    inspect = matches[0]
    context = docker[docker.index('--context') + 1]
    if (inspect['Driver'] != 'docker-container' or len(inspect['Nodes']) != 1 or
            inspect['Nodes'][0]['Name'] != options.builder + '0'):
        raise ValueError('bounded build requires one explicit docker-container builder')
    endpoint = inspect['Nodes'][0]['Endpoint']
    if endpoint != context:
        # Buildx normalizes the default context to its Docker endpoint URI.
        # Resolve that exact context, rather than accepting an arbitrary URI
        # or relaxing the single-daemon ownership check.
        if not endpoint.startswith(('unix://', 'tcp://', 'ssh://', 'npipe://')):
            raise ValueError('bounded build requires one explicit docker-container builder')
        actual = json.loads(subprocess.check_output(
            [*docker, 'context', 'inspect', context, '--format', '{{json .Endpoints.docker.Host}}'],
            text=True, timeout=30))
        if not isinstance(actual, str) or endpoint != actual:
            raise ValueError('bounded build requires one explicit docker-container builder')
    container = json.loads(subprocess.check_output(
        [*docker, 'inspect', 'buildx_buildkit_' + options.builder + '0'], text=True, timeout=30))[0]
    config = container['HostConfig']
    # Quotas apply to BuildKit and its build steps together, not just Go.
    if (not container['State']['Running'] or not 0 < config['Memory'] <= 3 * 1024**3 or
            config['CpuPeriod'] <= 0 or not 0 < config['CpuQuota'] <= 3 * config['CpuPeriod']):
        raise ValueError('builder does not enforce the 3 CPU / 3 GiB ceiling')
    return {'name': options.builder, 'container_id': container['Id'], 'image_id': container['Image'],
            'memory': config['Memory'], 'cpu_period': config['CpuPeriod'], 'cpu_quota': config['CpuQuota']}


def arguments(options):
    architecture(options.platform)
    result = ['--platform=' + options.platform]
    if options.network == 'host':
        result += ['--network=host', '--allow=network.host']
    builder = getattr(options, 'builder', None)
    if builder:
        if not re.fullmatch(r'[a-z][a-z0-9-]{0,62}', builder):
            raise ValueError('expected one explicit Buildx builder name')
        result += ['--builder=' + builder]
    elif options.bounded_build:
        raise ValueError('--bounded-build requires --builder with verified container limits')
    return result
