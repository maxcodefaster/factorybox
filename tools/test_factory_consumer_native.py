import copy
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


class RepeatedConsumerTests(unittest.TestCase):
    def setUp(self):
        self.result = {'sandboxID': 'original', 'start': {'record': {'runtime_id': 'original-runtime'}}}
        self.previous = {'nonce': 'original-memory', 'pid': 17, 'count': 1, 'editor': 'retained-source'}
        self.calls = []
        self.work = dict(self.previous)

    def pause(self, entry):
        self.calls.append(('pause', entry['snapshot_id']))
        return {'record': {'id': 'original', 'runtime_id': 'original-runtime', 'phase': 'paused',
                           'pause': {'snapshot_id': entry['snapshot_id'], 'completed_at': 'committed',
                                     'after': {'anon': 0},
                                     'snapshot': {'file_backed': True, 'resident_bytes_after': 0}}}}

    def resume(self, entry):
        self.calls.append(('resume', entry['snapshot_id']))
        return {'record': {'id': 'original', 'runtime_id': 'original-runtime', 'phase': 'running',
                           'pause': {'snapshot_id': entry['snapshot_id'],
                                     'resume': {'completed_at': 'committed'}}}}

    def useful(self):
        self.calls.append(('useful', None))
        self.work['count'] += 1
        return dict(self.work)

    def inspect(self):
        self.calls.append(('inspect', None))
        return {'State': {'Running': True}, 'RestartCount': 0}

    def run_cycles(self, **callbacks):
        return consumer.repeated_consumer_cycles(
            4, self.result, self.previous, callbacks.get('pause', self.pause),
            callbacks.get('resume', self.resume), callbacks.get('useful', self.useful),
            callbacks.get('inspect', self.inspect), lambda stage:self.calls.append(('phase', stage)),
            retire=callbacks.get('retire'))

    def test_reuses_the_original_stack_and_confirms_each_independent_snapshot(self):
        after = self.run_cycles()
        self.assertEqual(after, {**self.previous, 'count': 4})
        entries = self.result['repeat_cycles']
        self.assertEqual([entry['cycle'] for entry in entries], [2, 3, 4])
        self.assertEqual(len({entry['snapshot_id'] for entry in entries}), 3)
        self.assertTrue(all(entry['pause_attempted'] and entry['resume_attempted'] and entry['passed']
                            for entry in entries))
        self.assertEqual([call[0] for call in self.calls],
                         ['pause', 'resume', 'useful', 'inspect', 'phase'] * 3)

    def test_failed_resume_is_retained_without_retry_or_another_pause(self):
        def refused(entry):
            self.calls.append(('resume', entry['snapshot_id']))
            raise ValueError('restored original cgroup is not populated and frozen')
        with self.assertRaisesRegex(ValueError, 'original cgroup'):
            self.run_cycles(resume=refused)
        self.assertEqual([call[0] for call in self.calls], ['pause', 'resume'])
        self.assertEqual(len(self.result['repeat_cycles']), 1)
        self.assertTrue(self.result['repeat_cycles'][0]['resume_attempted'])
        self.assertNotIn('passed', self.result['repeat_cycles'][0])

    def test_changed_identity_or_unreleased_ram_refuses_before_resume(self):
        entry = {'snapshot_id': 'a' * 32}
        view = self.pause(entry)
        changes = [
            ('id', 'foreign'), ('runtime_id', 'foreign-runtime'), ('phase', 'running'),
        ]
        for key, value in changes:
            broken = copy.deepcopy(view)
            broken['record'][key] = value
            with self.assertRaisesRegex(ValueError, 'original Sandbox'):
                consumer.require_paused_consumer(broken, 'original', 'original-runtime', entry['snapshot_id'])
        for path, value in [
                (['snapshot_id'], 'b' * 32), (['completed_at'], ''),
                (['after', 'anon'], 4096), (['snapshot', 'file_backed'], False),
                (['snapshot', 'resident_bytes_after'], 4096)]:
            broken = copy.deepcopy(view)
            target = broken['record']['pause']
            for name in path[:-1]:
                target = target[name]
            target[path[-1]] = value
            with self.assertRaisesRegex(ValueError, 'release its RAM'):
                consumer.require_paused_consumer(broken, 'original', 'original-runtime', entry['snapshot_id'])
        def retained_ram(actual):
            broken = self.pause(actual)
            broken['record']['pause']['after']['anon'] = 4096
            return broken
        with self.assertRaises(ValueError):
            self.run_cycles(pause=retained_ram)
        self.assertEqual([call[0] for call in self.calls], ['pause', 'pause'])

    def test_restart_or_changed_source_stops_without_another_cycle(self):
        for key, value in [('nonce', 'replacement'), ('pid', 18), ('count', 1), ('editor', 'foreign')]:
            self.setUp()
            def changed():
                return {**self.useful(), key:value}
            with self.assertRaisesRegex(ValueError, 'app memory'):
                self.run_cycles(useful=changed)
            self.assertEqual([call[0] for call in self.calls], ['pause', 'resume', 'useful'])
        self.setUp()
        with self.assertRaisesRegex(ValueError, 'Docker child'):
            self.run_cycles(inspect=lambda:{'State': {'Running': True}, 'RestartCount': 1})
        self.assertEqual(len(self.result['repeat_cycles']), 1)

    def test_single_cycle_adds_no_work_and_invalid_counts_refuse(self):
        self.assertIs(consumer.repeated_consumer_cycles(
            1, self.result, self.previous, self.pause, self.resume, self.useful, self.inspect, None), self.previous)
        self.assertEqual(self.calls, [])
        for count in [0, -1, True]:
            with self.assertRaises(ValueError):
                consumer.repeated_consumer_cycles(
                    count, self.result, self.previous, self.pause, self.resume, self.useful, self.inspect, None)

    def test_retirement_runs_only_after_original_app_and_child_preservation(self):
        def retire(entry):
            self.assertEqual(self.calls[-2:], [('useful', None), ('inspect', None)])
            self.assertTrue(entry['retirement_attempted'])
            self.calls.append(('retire', entry['snapshot_id']))
            return {'completed_at': 'retired'}
        self.run_cycles(retire=retire)
        self.assertEqual([call[0] for call in self.calls],
                         ['pause', 'resume', 'useful', 'inspect', 'retire', 'phase'] * 3)
        self.assertTrue(all(entry['retirement']['completed_at']
                            for entry in self.result['repeat_cycles']))
        self.setUp()
        def refused(_entry):
            raise ValueError('checkpoint remains protected')
        with self.assertRaisesRegex(ValueError, 'protected'):
            self.run_cycles(retire=refused)
        self.assertEqual(len(self.result['repeat_cycles']), 1)
        self.assertTrue(self.result['repeat_cycles'][0]['retirement_attempted'])
        self.assertNotIn('passed', self.result['repeat_cycles'][0])


class ConsumerCheckpointRetirementTests(unittest.TestCase):
    def setUp(self):
        self.snapshot = 'a' * 32
        self.pause = {'record': {
            'id': 'original', 'runtime_id': 'original-runtime', 'owner': 'b' * 32,
            'phase': 'paused', 'pause': {
                'snapshot_id': self.snapshot, 'checkpoint_sha256': 'c' * 64,
                'completed_at': 'committed', 'after': {'anon': 0},
                'snapshot': {'file_backed': True, 'resident_bytes_after': 0},
            },
        }}
        self.resume = copy.deepcopy(self.pause)
        self.resume['record']['phase'] = 'running'
        self.resume['record']['pause']['resume'] = {'completed_at': 'resumed'}
        self.resume['runtime'] = {'status': 'running'}
        self.calls = []

    def execute(self, request):
        self.calls.append(request)
        return {'version': 1, 'request': request, 'completed_at': 'retired',
                'before': {'filesystem_available_bytes': 1024},
                'after': {'filesystem_available_bytes': 8192}}

    def retire(self, execute=None):
        return consumer.retire_resumed_checkpoint(
            self.pause, self.resume, 'original', 'original-runtime', execute or self.execute)

    def test_only_the_exact_verified_resumed_checkpoint_can_release_its_payload(self):
        before = copy.deepcopy((self.pause, self.resume))
        self.assertEqual(self.retire()['completed_at'], 'retired')
        self.assertEqual(self.calls, [{
            'source_id': 'original', 'snapshot_id': self.snapshot,
            'source_owner': 'b' * 32, 'checkpoint_sha256': 'c' * 64,
        }])
        self.assertEqual((self.pause, self.resume), before)

    def test_wrong_identity_unreleased_ram_or_incomplete_resume_refuses_before_mutation(self):
        for changed in [
            lambda: self.pause['record'].update(id='foreign'),
            lambda: self.pause['record'].update(owner=''),
            lambda: self.pause['record']['pause'].update(checkpoint_sha256='unknown'),
            lambda: self.pause['record']['pause']['after'].update(anon=4096),
            lambda: self.resume['record'].update(runtime_id='replacement'),
            lambda: self.resume['record'].update(owner='different-owner'),
            lambda: self.resume['record']['pause'].update(snapshot_id='d' * 32),
            lambda: self.resume['record']['pause']['resume'].update(completed_at=''),
            lambda: self.resume['runtime'].update(status='stopped'),
        ]:
            self.setUp()
            changed()
            with self.assertRaises(ValueError):
                self.retire()
            self.assertEqual(self.calls, [])

    def test_lost_wrong_or_nonreclaiming_retirement_never_authorizes_another_cycle(self):
        for changed in [
            lambda value: value.update(completed_at=''),
            lambda value: value['request'].update(source_owner='foreign'),
            lambda value: value['after'].update(filesystem_available_bytes=1024),
            lambda value: value['after'].update(filesystem_available_bytes=True),
        ]:
            self.setUp()
            def execute(request):
                result = self.execute(copy.deepcopy(request))
                changed(result)
                return result
            with self.assertRaises(ValueError):
                self.retire(execute)
            self.assertEqual(len(self.calls), 1)
        self.setUp()
        def lost(request):
            self.calls.append(request)
            raise ValueError('lost native retirement response')
        with self.assertRaisesRegex(ValueError, 'lost native'):
            self.retire(lost)
        self.assertEqual(len(self.calls), 1)


class ResumeImageRetirementTests(unittest.TestCase):
    def setUp(self):
        self.snapshot = 'a' * 32
        self.previous = {'record': {'id': 'original', 'runtime_id': 'runtime', 'owner': 'b' * 32,
            'phase': 'running', 'pause': {'snapshot_id': self.snapshot,
                'checkpoint_sha256': 'c' * 64, 'completed_at': 'paused',
                'resume': {'completed_at': 'resumed'}}}, 'runtime': {'status': 'running'}}
        self.latest = copy.deepcopy(self.previous)
        self.latest['record']['phase'] = 'paused'
        self.latest['record']['pause'] = {'snapshot_id': 'd' * 32, 'checkpoint_sha256': 'e' * 64,
            'completed_at': 'new-pause',
            'after': {'anon': 0}, 'snapshot': {'file_backed': True, 'resident_bytes_after': 0}}
        self.latest.pop('runtime')
        self.calls = []

    def execute(self, request):
        self.calls.append(request)
        return {'version': 1, 'payload_kind': 'resume-images', 'request': request,
            'completed_at': 'retired', 'before': {'filesystem_available_bytes': 10},
            'after': {'filesystem_available_bytes': 100}}

    def retire(self, execute=None):
        return consumer.retire_obsolete_resume_images(
            self.latest, self.previous, 'original', 'runtime', execute or self.execute)

    def test_later_complete_pause_authorizes_only_its_preceding_completed_resume(self):
        before = copy.deepcopy((self.latest, self.previous))
        self.assertEqual(self.retire()['payload_kind'], 'resume-images')
        self.assertEqual(self.calls[0]['snapshot_id'], self.snapshot)
        self.assertEqual((self.latest, self.previous), before)

    def test_active_incomplete_or_foreign_resume_never_reaches_the_mutation(self):
        for change in [
                lambda: self.latest['record']['pause'].update(snapshot_id=self.snapshot),
                lambda: self.latest['record'].update(id='foreign'),
                lambda: self.latest['record'].update(owner='e' * 32),
                lambda: self.latest['record']['pause'].update(completed_at=''),
                lambda: self.latest['record']['pause'].update(checkpoint_sha256='unknown'),
                lambda: self.latest['record']['pause']['after'].update(anon=1),
                lambda: self.previous['record']['pause']['resume'].update(completed_at=''),
                lambda: self.previous['record']['pause'].update(checkpoint_sha256='unknown')]:
            self.setUp()
            change()
            with self.assertRaises(ValueError):
                self.retire()
            self.assertEqual(self.calls, [])

    def test_confirmed_delete_can_retire_the_last_resume(self):
        self.latest = copy.deepcopy(self.previous)
        self.latest.pop('runtime')
        self.latest['record'].update(phase='deleted', delete={'completed_at': 'deleted'})
        self.assertEqual(self.retire()['completed_at'], 'retired')

    def test_wrong_or_nonreclaiming_output_stops_the_sequence(self):
        for change in [
                lambda value: value.update(payload_kind='checkpoint'),
                lambda value: value.update(completed_at=''),
                lambda value: value['request'].update(snapshot_id='e' * 32),
                lambda value: value['after'].update(filesystem_available_bytes=10),
                lambda value: value['after'].update(filesystem_available_bytes=True)]:
            self.setUp()
            def execute(request):
                value = self.execute(copy.deepcopy(request))
                change(value)
                return value
            with self.assertRaises(ValueError):
                self.retire(execute)
            self.assertEqual(len(self.calls), 1)


class NativeFactoryConsumerInputTests(unittest.TestCase):
    def test_reserve_retains_its_slot_sandbox_when_an_attempt_is_assigned(self):
        slot = 'a' * 32
        attempt = '11111111-2222-4333-8444-555555555555'
        self.assertEqual(consumer.consumer_sandbox_id(slot, attempt, True), 'runtime-'+slot)
        self.assertEqual(consumer.consumer_sandbox_id(slot, attempt, False),
                         'runtime-'+attempt.replace('-', ''))
        later_attempt = '66666666-7777-4888-8999-aaaaaaaaaaaa'
        self.assertEqual(consumer.consumer_sandbox_id(slot, later_attempt, True), 'runtime-'+slot)

    def test_diagnostic_success_never_becomes_native_acceptance(self):
        for diagnostic in [False, True]:
            result = {'passed': True, 'cleanup': True, 'host_sysctls_unchanged': True,
                      'inputs_unchanged': True, 'diagnostic_only': diagnostic}
            self.assertTrue(consumer.settle_lane_result(result))
            self.assertEqual(result['passed'], not diagnostic)
            if diagnostic:
                self.assertTrue(result['diagnostic_control_passed'])
            for failed in ['passed', 'cleanup', 'host_sysctls_unchanged', 'inputs_unchanged']:
                broken = {**result, 'passed': True, failed: False}
                self.assertFalse(consumer.settle_lane_result(broken))
                self.assertFalse(broken['passed'])
                if diagnostic:
                    self.assertFalse(broken['diagnostic_control_passed'])

    def test_preparation_delivers_only_the_selected_sandbox_files_and_release_has_no_credentials(self):
        files = {"slot.json": '{"id":"slot"}', "assignment.json": '{"runtimeAttemptId":"assigned"}',
                 "parent-token": "private-parent", "child-token": "private-child"}
        self.assertEqual(consumer.preparation_assignment(files, True),
                         {"slot.json": files["slot.json"]})
        normal = consumer.preparation_assignment(files, False)
        self.assertEqual(set(normal), {"assignment.json", "parent-token", "child-token"})
        self.assertNotIn("slot.json", normal)
        request = {"version": 3, "sandboxID": "owned", "assignment": normal}
        self.assertEqual(consumer.preparation_identity(request), {"version": 3, "sandboxID": "owned"})
        self.assertEqual(request["assignment"], normal)

    def test_readiness_uses_http_from_a_separate_owned_cgroup_before_any_worker_exec(self):
        calls = []
        def verify():
            calls.append('verified')
        def run(arguments, **options):
            self.assertEqual(calls, ['verified'])
            self.assertEqual(arguments[:6],
                             ['exec', 'owned-helper', 'nsenter', '--net=/proc/123/ns/net', '--', 'python3'])
            self.assertNotIn('owned-worker', arguments)
            self.assertIn('/readyz', arguments[-1])
            self.assertEqual(options, {'check': False, 'timeout': 5})
            calls.append('http')
            return '204\n'
        self.assertTrue(consumer.worker_http_ready(run, 'owned-worker', 'owned-helper', 123, verify))
        self.assertEqual(calls, ['verified', 'http'])

    def test_readiness_rejects_nonready_responses_and_identity_changes_without_worker_exec(self):
        for response in ['200\n', '503\n', '', '204 extra\n']:
            self.assertFalse(consumer.worker_http_ready(
                lambda *args, **kwargs: response, 'worker', 'helper', 123, lambda: None))
        def refused():
            raise RuntimeError('original worker exited')
        with patch('factory_consumer_native.reference.docker') as command:
            with self.assertRaisesRegex(RuntimeError, 'exited'):
                consumer.worker_http_ready(command, 'worker', 'helper', 123, refused)
            command.assert_not_called()
            for pid, helper in [(0, 'helper'), (123, 'worker')]:
                with self.assertRaisesRegex(ValueError, 'separate owned'):
                    consumer.worker_http_ready(command, 'worker', helper, pid, lambda: None)
            command.assert_not_called()

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
