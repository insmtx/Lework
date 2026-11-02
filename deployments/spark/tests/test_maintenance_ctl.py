import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))


class MaintenanceControlTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        import maintenance_ctl
        cls.ctl = maintenance_ctl

    def test_initialize_pause_resume_and_stale_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'maintenance.json'
            initial = self.ctl.operate(path, 'host', 'init')
            paused = self.ctl.operate(path, 'host', 'pause', initial['revision'], 'op-1')
            self.assertTrue(paused['active'])
            self.assertEqual(self.ctl.operate(path, 'host', 'init'), paused)
            for rev, op in [(initial['revision'], 'op-1'), (paused['revision'], 'other')]:
                with self.assertRaises(ValueError):
                    self.ctl.operate(path, 'host', 'resume', rev, op)
            self.assertTrue(self.ctl.operate(path, 'host', 'status')['active'])
            resumed = self.ctl.operate(path, 'host', 'resume', paused['revision'], 'op-1')
            self.assertFalse(resumed['active'])
            with self.assertRaises(ValueError):
                self.ctl.operate(path, 'other', 'status')

    def test_initialize_never_recreates_lost_or_corrupt_state(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'maintenance.json'
            self.ctl.operate(path, 'host', 'init')
            self.ctl.operate(path, 'host', 'pause', 1, 'op')
            path.unlink()
            with self.assertRaises(ValueError):
                self.ctl.operate(path, 'host', 'init')
            self.assertFalse(path.exists())
            path.write_text('{}')
            with self.assertRaises(ValueError):
                self.ctl.operate(path, 'host', 'init')
            self.assertEqual(path.read_text(), '{}')

    def test_interrupted_first_initialization_stays_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'maintenance.json'
            with patch.object(self.ctl.StateStore, 'initialize', side_effect=OSError('disk')):
                with self.assertRaises(OSError):
                    self.ctl.operate(path, 'host', 'init')
            with self.assertRaises(ValueError):
                self.ctl.operate(path, 'host', 'init')

    def test_cli_refuses_nonroot_without_writing_and_sanitizes_failures(self):
        for uid in [1000, 0]:
            with patch.object(self.ctl.os, 'geteuid', return_value=uid), \
                    patch.object(self.ctl, 'validate_directory', side_effect=OSError('/private/secret')), \
                    patch.object(self.ctl, 'operate') as operation, \
                    contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertNotEqual(self.ctl.main(['init']), 0)
                operation.assert_not_called()
                self.assertNotIn('secret', output.getvalue())
                self.assertEqual(json.loads(output.getvalue())['outcome'], 'not_attempted')

    def test_postwrite_readback_failure_reports_unknown_outcome(self):
        with patch.object(self.ctl.os, 'geteuid', return_value=0), \
                patch.object(self.ctl, 'validate_directory'), \
                patch.object(self.ctl, 'local_host_id', return_value='host'), \
                patch.object(self.ctl, 'operate', side_effect=OSError('/private/secret')), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertNotEqual(self.ctl.main(['pause', '--expected-revision', '1', '--operation-id', 'op']), 0)
            result=json.loads(output.getvalue())
            self.assertEqual(result['outcome'], 'unknown')
            self.assertNotIn('secret', output.getvalue())

    def test_cli_does_not_accept_custom_target_or_power_commands(self):
        with contextlib.redirect_stderr(io.StringIO()):
            for args in [['shutdown'], ['status','--host-id','other'], ['status','--state-path','/tmp/a']]:
                with self.assertRaises(SystemExit):
                    self.ctl.main(args)

    def test_directory_rejects_symlink_nonroot_and_group_write(self):
        import stat
        from types import SimpleNamespace
        for mode, uid in [(stat.S_IFLNK | 0o777, 0), (stat.S_IFDIR | 0o750, 1000),
                          (stat.S_IFDIR | 0o770, 0), (stat.S_IFDIR | 0o757, 0)]:
            with patch.object(self.ctl.Path, 'lstat', return_value=SimpleNamespace(st_mode=mode, st_uid=uid)):
                with self.assertRaises(ValueError): self.ctl.validate_directory()
        with patch.object(self.ctl.Path, 'lstat', return_value=SimpleNamespace(st_mode=stat.S_IFDIR | 0o2750, st_uid=0)):
            self.ctl.validate_directory()

    def test_installed_entrypoint_supports_isolated_python(self):
        import subprocess
        result = subprocess.run([sys.executable, '-I', str(ROOT/'maintenance_ctl.py'), '--help'], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('pause', result.stdout)
