import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

MODULE = Path(__file__).resolve().parents[1] / 'shutdown_plan.py'


class ShutdownPlanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('shutdown_plan', MODULE)
        cls.plan = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.plan)

    def fixtures(self):
        registry = {'version': 1, 'host_id': 'host-1', 'targets': [
            {'id': 'database', 'kind': 'docker', 'component_id': 'db-container',
             'scope': 'local', 'depends_on': [], 'stop_strategy': 'docker_graceful',
             'restore_strategy': 'previous_state'},
            {'id': 'worker', 'kind': 'kubernetes', 'component_id': 'app/worker-pod',
             'scope': 'local', 'depends_on': ['database'], 'stop_strategy': 'kubernetes_scaledown',
             'restore_strategy': 'previous_state'},
            {'id': 'model', 'kind': 'docker', 'component_id': 'model-container',
             'scope': 'local', 'depends_on': [], 'stop_strategy': 'docker_graceful',
             'restore_strategy': 'previous_state'},
        ]}
        snapshot = {'protocol_version': 1, 'host_id': 'host-1', 'boot_id': 'boot-1',
                    'observed_at': 100, 'inventory_complete': True,
                    'components': [
                        {'kind': 'docker', 'id': 'db-container', 'name': 'db', 'state': 'running'},
                        {'kind': 'kubernetes', 'id': 'app/worker-pod', 'name': 'worker', 'state': 'Running'},
                        {'kind': 'docker', 'id': 'model-container', 'name': 'model', 'state': 'exited'},
                    ]}
        return registry, snapshot

    def build(self, registry=None, snapshot=None, now=101):
        r, s = self.fixtures()
        return self.plan.build_plan(registry or r, snapshot or s, now)

    def test_dependency_stop_reverse_restore_and_never_start_stopped_model(self):
        p = self.build()
        self.assertEqual(p['stop_order'], ['worker', 'database'])
        self.assertEqual(p['restore_order'], ['database', 'worker'])
        self.assertEqual(p['keep_stopped'], ['model'])
        self.assertTrue(p['policy_valid'])
        self.assertFalse(p['executable'])
        self.assertIn('execution_unimplemented', p['blockers'])
        self.assertEqual(p['boot_id'], 'boot-1')

    def test_omitted_workload_or_recreated_pod_blocks_and_has_no_orders(self):
        r, s = self.fixtures()
        for changed in [s['components'][:2], s['components'] + [dict(kind='docker', id='extra', name='extra', state='exited')]]:
            ss = dict(s, components=changed)
            p = self.build(r, ss)
            self.assertFalse(p['policy_valid'])
            self.assertEqual(p['stop_order'], [])
        s['components'][1]['id'] = 'app/new-pod'
        p = self.build(r, s)
        self.assertIn('unregistered_workloads', p['blockers'])
        self.assertIn('registered_target_missing', p['blockers'])

    def test_invalid_schema_and_unsafe_targets_rejected(self):
        base, s = self.fixtures()
        changes = [lambda r: r.update(version=True), lambda r: r.update(command='poweroff'),
                   lambda r: r['targets'][0].update(scope='distributed'),
                   lambda r: r['targets'][0].update(stop_strategy='kill'),
                   lambda r: r['targets'][0].update(restore_strategy='always_start'),
                   lambda r: r['targets'][0].update(depends_on=['missing']),
                   lambda r: r['targets'][0].update(depends_on=['database']),
                   lambda r: r['targets'].append(copy.deepcopy(r['targets'][0])),
                   lambda r: r['targets'][1].update(kind='docker', component_id='db-container'),
                   lambda r: r['targets'][0].update(depends_on=['model', 'model'])]
        for change in changes:
            r = copy.deepcopy(base)
            change(r)
            with self.subTest(r=r), self.assertRaises(ValueError):
                self.build(r, s)

    def test_cycles_and_stopped_dependencies_block(self):
        r, s = self.fixtures()
        r['targets'][0]['depends_on'] = ['worker']
        p = self.build(r, s)
        self.assertIn('dependency_cycle', p['blockers'])
        self.assertEqual(p['stop_order'], [])
        r, s = self.fixtures()
        r['targets'][1]['depends_on'] = ['model']
        self.assertIn('dependency_not_running', self.build(r, s)['blockers'])

    def test_untrusted_observations_fail_closed(self):
        r, base = self.fixtures()
        changes = [dict(host_id='other'), dict(boot_id=''), dict(observed_at=70),
                   dict(observed_at=107), dict(observed_at=True), dict(inventory_complete=False),
                   dict(inventory_complete='true'), dict(protocol_version=True)]
        for change in changes:
            with self.subTest(change=change):
                p = self.build(r, dict(base, **change))
                self.assertFalse(p['policy_valid'])
                self.assertEqual(p['restore_order'], [])
        for state in ['paused', 'restarting', 'Pending', 'unknown']:
            s = copy.deepcopy(base)
            s['components'][0]['state'] = state
            self.assertIn('workload_state_unknown', self.build(r, s)['blockers'])

    def test_duplicate_or_malformed_inventory_cannot_cover_registration(self):
        r, s = self.fixtures()
        for components in [s['components'] * 2, [{}], 'bad', None]:
            self.assertFalse(self.build(r, dict(s, components=components))['policy_valid'])

    def test_extreme_timestamp_is_invalid_without_arithmetic_overflow(self):
        r, s = self.fixtures()
        p = self.build(r, dict(s, observed_at=10 ** 400), now=101.0)
        self.assertFalse(p['policy_valid'])
        self.assertIn('snapshot_invalid_or_stale', p['blockers'])

    def test_plan_is_deterministic_but_bound_to_input_boot_and_policy(self):
        r, s = self.fixtures()
        p = self.build(r, s)
        self.assertEqual(p, self.build(dict(r, targets=list(reversed(r['targets']))), s))
        self.assertNotEqual(p['plan_digest'], self.build(r, dict(s, boot_id='boot-2'))['plan_digest'])
        r['targets'][1]['depends_on'] = []
        self.assertNotEqual(p['plan_digest'], self.build(r, s)['plan_digest'])

    def test_cli_blocks_without_executing_and_redacts_invalid_input(self):
        r, s = self.fixtures()
        with tempfile.TemporaryDirectory() as d:
            rp, sp = Path(d) / 'r.json', Path(d) / 's.json'
            rp.write_text(json.dumps(r)); sp.write_text(json.dumps(s))
            cmd = [sys.executable, '-I', str(MODULE), '--registry', str(rp), '--snapshot', str(sp)]
            result = subprocess.run(cmd, capture_output=True, text=True)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertFalse(json.loads(result.stdout)['executable'])
            self.assertEqual(sorted(p.name for p in Path(d).iterdir()), ['r.json', 's.json'])
            for data in ['{"version":1,"version":2,"secret":"do-not-echo"}',
                         json.dumps(dict(r, host_id='host\ud800')),
                         'x' * (1024 * 1024 + 1)]:
                rp.write_text(data)
                result = subprocess.run(cmd, capture_output=True, text=True)
                self.assertEqual(result.returncode, 2)
                self.assertNotIn('do-not-echo', result.stdout + result.stderr)
                self.assertNotIn('Traceback', result.stderr)


if __name__ == '__main__':
    unittest.main()
