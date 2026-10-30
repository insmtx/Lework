"""Exercise recovery paths without changing the developer's network."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

MODULE = Path(__file__).resolve().parents[1] / 'install_wired.py'
spec = importlib.util.spec_from_file_location('install_wired', MODULE)
wired = importlib.util.module_from_spec(spec) if MODULE.exists() else None
if wired:
    spec.loader.exec_module(wired)

OLD = '6b864655-27f4-3c72-b22e-8ce1d7dfd5e5'
OTHER = 'a7c53165-dcc8-43df-8657-b2e286b728be'


class FakeHost:
    """Only the external NM/systemd boundary is replaced; state files are real."""
    def __init__(self):
        self.profiles = {OLD: {
            'connection.uuid': OLD, 'connection.type': '802-3-ethernet',
            'connection.interface-name': 'enP7s7', 'connection.id': '有线连接 3',
            'connection.autoconnect': 'yes', 'connection.autoconnect-priority': '-999',
            'ipv4.method': 'manual', 'ipv4.addresses': '172.16.0.64/16',
            'ipv4.gateway': '172.16.0.1',
        }}
        self.active = ''
        self.carrier = False
        self.enabled = False
        self.running = False
        self.fail_enable = False
        self.fail_delete = False
        self.activations = []

    def preflight(self, interface):
        if interface != 'enP7s7':
            raise RuntimeError('wrong interface')

    def profile(self, uid):
        return self.profiles.get(uid)

    def active_uuid(self, interface):
        return self.active

    def has_carrier(self, interface):
        return self.carrier

    def create(self, properties):
        uid = properties['connection.uuid']
        if uid in self.profiles:
            raise RuntimeError('duplicate UUID')
        self.profiles[uid] = dict(properties)

    def autoconnect(self, uid, value):
        self.profiles[uid]['connection.autoconnect'] = value

    def activate(self, uid, interface):
        if not self.carrier:
            raise RuntimeError('no carrier')
        self.activations.append(uid)
        self.active = uid

    def delete(self, uid):
        if self.fail_delete:
            raise RuntimeError('delete failed')
        self.profiles.pop(uid, None)
        if self.active == uid:
            self.active = ''

    def enable_service(self):
        self.enabled = True
        if self.fail_enable:
            raise RuntimeError('service failed')
        self.running = True

    def disable_service(self):
        self.enabled = False
        self.running = False

    def reload(self):
        pass


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(wired, 'persistent wired installer is not implemented')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.host = FakeHost()
        self.original = copy.deepcopy(self.host.profiles[OLD])
        for name, content in {
            'etc/leros-discovery.conf': 'LAN_INTERFACE=wlP9s9\nSERVICE_PORT=38081\nSERVICE_SCHEME=http\n',
            'var/lib/leros-discovery/id': '5093c886-d4f7-4a68-bc71-2450725c6e1c\n',
            'usr/local/lib/leros-discovery/discovery.py': '# installed publisher\n',
        }.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        self.installer = wired.Installer(self.host, self.root)

    def install(self):
        self.installer.install('enP7s7', OLD)
        return json.loads((self.root / 'var/lib/leros-wired/state.json').read_text())

    def test_persistent_direct_link_preserves_original_addresses(self):
        state = self.install()
        current = self.host.profiles[OLD]
        expected = dict(self.original, **{'connection.autoconnect': 'no'})
        self.assertEqual(current, expected)
        new = self.host.profiles[state['managed_uuid']]
        self.assertEqual(new['ipv4.method'], 'link-local')
        self.assertEqual(new['ipv6.method'], 'link-local')
        self.assertEqual(new['ipv4.never-default'], 'yes')
        self.assertEqual(new['ipv6.never-default'], 'yes')
        self.assertNotIn('ipv4.gateway', new)
        self.assertEqual(new['connection.autoconnect'], 'yes')
        self.assertEqual(new['connection.interface-name'], 'enP7s7')
        self.assertGreater(int(new['connection.autoconnect-priority']), 0)
        self.assertTrue(self.host.running)
        self.assertEqual(self.host.activations, [])
        self.assertIn('LAN_INTERFACE=enP7s7', (self.root / 'etc/leros-discovery-wired.conf').read_text())

    def test_repeat_install_keeps_uuid_and_original_rollback_value(self):
        first = self.install()
        second = self.install()
        self.assertEqual(first, second)
        self.assertEqual(len(self.host.profiles), 2)
        self.installer.rollback()
        self.assertEqual(self.host.profiles, {OLD: self.original})
        self.assertFalse(self.host.enabled)
        self.installer.rollback()

    def test_disabled_original_stays_disabled_after_rollback(self):
        self.host.profiles[OLD]['connection.autoconnect'] = 'no'
        self.install()
        self.installer.rollback()
        self.assertEqual(self.host.profiles[OLD]['connection.autoconnect'], 'no')

    def test_connected_install_and_rollback_reactivate_old_connection(self):
        self.host.carrier = True
        self.host.active = OLD
        state = self.install()
        self.assertEqual(self.host.active, state['managed_uuid'])
        self.installer.rollback()
        self.assertEqual(self.host.active, OLD)

    def test_rollback_does_not_replace_user_selected_connection(self):
        self.install()
        self.host.carrier = True
        self.host.active = OTHER
        self.installer.rollback()
        self.assertEqual(self.host.active, OTHER)
        self.assertEqual(self.host.activations, [])

    def test_rollback_respects_connection_changed_during_cleanup(self):
        state = self.install()
        self.host.carrier = True
        self.host.active = state['managed_uuid']
        delete = self.host.delete
        def user_switches(uid):
            delete(uid)
            self.host.active = OTHER
        self.host.delete = user_switches
        self.installer.rollback()
        self.assertEqual(self.host.active, OTHER)
        self.assertEqual(self.host.activations, [])

    def test_rollback_refuses_managed_profile_rebound_to_cluster(self):
        state = self.install()
        self.host.profiles[state['managed_uuid']]['connection.interface-name'] = 'enp1s0f0np0'
        before = copy.deepcopy(self.host.profiles)
        with self.assertRaises(RuntimeError):
            self.installer.rollback()
        self.assertEqual(self.host.profiles, before)
        self.assertTrue((self.root / 'var/lib/leros-wired/state.json').exists())

    def test_service_failure_restores_network_and_removes_owned_files(self):
        self.host.fail_enable = True
        with self.assertRaisesRegex(RuntimeError, 'service failed'):
            self.install()
        self.assertEqual(self.host.profiles, {OLD: self.original})
        self.assertFalse((self.root / 'etc/leros-discovery-wired.conf').exists())
        self.assertFalse((self.root / 'var/lib/leros-wired/state.json').exists())

    def test_failed_recovery_keeps_state_for_retry(self):
        self.host.fail_enable = True
        self.host.fail_delete = True
        with self.assertRaises(RuntimeError):
            self.install()
        self.assertTrue((self.root / 'var/lib/leros-wired/state.json').exists())
        self.host.fail_delete = False
        self.installer.rollback()
        self.assertEqual(self.host.profiles, {OLD: self.original})

    def test_interrupted_install_cannot_overwrite_recovery_baseline(self):
        self.host.enable_service = lambda: (_ for _ in ()).throw(KeyboardInterrupt())
        with self.assertRaises(KeyboardInterrupt):
            self.install()
        before = (self.root / 'var/lib/leros-wired/state.json').read_text()
        with self.assertRaisesRegex(RuntimeError, 'rollback'):
            self.install()
        self.assertEqual((self.root / 'var/lib/leros-wired/state.json').read_text(), before)
        self.installer.rollback()
        self.assertEqual(self.host.profiles, {OLD: self.original})

    def test_preflight_refuses_wrong_interface_or_profile_without_changes(self):
        for interface, uid in [('enp1s0f0np0', OLD), ('enP7s7', OTHER)]:
            with self.subTest(interface=interface, uid=uid):
                with self.assertRaises((ValueError, RuntimeError)):
                    self.installer.install(interface, uid)
                self.assertEqual(self.host.profiles, {OLD: self.original})
                self.assertFalse((self.root / 'var/lib/leros-wired/state.json').exists())
        self.host.profiles[OLD]['connection.interface-name'] = 'wlP9s9'
        with self.assertRaises((ValueError, RuntimeError)):
            self.installer.install('enP7s7', OLD)

    def test_foreign_service_config_is_not_overwritten(self):
        path = self.root / 'etc/leros-discovery-wired.conf'
        path.write_text('existing admin file')
        with self.assertRaises(RuntimeError):
            self.install()
        self.assertEqual(path.read_text(), 'existing admin file')
        self.assertEqual(self.host.profiles, {OLD: self.original})

    def test_check_is_read_only(self):
        self.installer.check('enP7s7', OLD)
        self.assertEqual(self.host.profiles, {OLD: self.original})
        self.assertFalse((self.root / 'var/lib/leros-wired').exists())


if __name__ == '__main__':
    unittest.main()
