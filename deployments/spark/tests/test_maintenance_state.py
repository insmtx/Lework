import importlib.util
from pathlib import Path
import tempfile
import unittest

class MaintenanceStateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec=importlib.util.spec_from_file_location('maintenance_state',Path(__file__).resolve().parents[1]/'maintenance_state.py')
        cls.module=importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.module)

    def test_pause_resume_restart_and_stale_request(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'state.json'
            store=self.module.StateStore(p,'host')
            initial=store.initialize()
            self.assertFalse(initial['active'])
            paused=store.change(1,'op-1',True)
            self.assertEqual(paused['revision'],2)
            self.assertTrue(self.module.StateStore(p,'host').read()['active'])
            with self.assertRaises(ValueError):
                store.change(1,'old',False)
            self.assertTrue(store.read()['active'])
            self.assertFalse(store.change(2,'op-1',False)['active'])

    def test_corrupt_or_foreign_file_never_auto_resets(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'state.json'
            store=self.module.StateStore(p,'host');store.initialize()
            with self.assertRaises(ValueError): self.module.StateStore(p,'other').read()
            p.write_text('{}')
            with self.assertRaises(ValueError): store.initialize()
            with self.assertRaises(ValueError): store.change(1,'op',False)
            self.assertEqual(p.read_text(),'{}')

    def test_failed_replace_preserves_pause(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as d:
            store=self.module.StateStore(Path(d)/'state.json','host')
            store.initialize();store.change(1,'op',True)
            with patch.object(self.module.os,'replace',side_effect=OSError('disk failure')):
                with self.assertRaises(OSError): store.change(2,'op',False)
            self.assertTrue(store.read()['active'])

    def test_boolean_protocol_version_is_rejected(self):
        import json
        with tempfile.TemporaryDirectory() as d:
            p = Path(d)/'state.json'
            store = self.module.StateStore(p,'host')
            state = store.initialize()
            state['version'] = True
            p.write_text(json.dumps(state))
            with self.assertRaises(ValueError): store.read()
