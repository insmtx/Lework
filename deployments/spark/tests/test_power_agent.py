import hashlib
import importlib.util
import json
from pathlib import Path
import socket
import tempfile
import threading
import unittest
from unittest import mock

MODULE = Path(__file__).resolve().parents[1] / 'power_agent.py'


class PowerAgentTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('power_agent', MODULE)
        cls.agent = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.agent)

    def collect(self, runner):
        host_id = hashlib.sha256(b'lework-spark:machine-1').hexdigest()
        def probe(argv):
            if 'node' in argv and 'get' in argv:
                return 'machine-1'
            return runner(argv)
        return self.agent.collect_status('master', runner=probe,
            identity=lambda: (host_id, 'boot-1', 'spark'), now=lambda: 1234)

    def test_container_identity_uses_operations_host_mount(self):
        with tempfile.TemporaryDirectory() as d:
            machine = Path(d) / 'machine-id'
            boot = Path(d) / 'boot-id'
            machine.write_text('spark-machine\n')
            boot.write_text('boot-123\n')
            real_path = Path
            def path(value):
                if value == '/proc/sys/kernel/random/boot_id':
                    return real_path(boot)
                return real_path(value)
            with mock.patch.dict('os.environ', {'SPARK_MACHINE_ID_PATH': str(machine), 'SPARK_HOSTNAME': 'master'}), mock.patch.object(self.agent, 'Path', side_effect=path):
                host, boot_id, hostname = self.agent.host_identity()
            self.assertEqual(host, hashlib.sha256(b'lework-spark:spark-machine').hexdigest())
            self.assertEqual(boot_id, 'boot-123')
            self.assertEqual(hostname, 'master')

    def test_container_probe_environment_survives_model_control_merge(self):
        calls = []
        host_id = hashlib.sha256(b'lework-spark:machine-1').hexdigest()
        def run(argv):
            calls.append(argv)
            return 'machine-1' if 'node' in argv and 'get' in argv else ''
        with mock.patch.dict('os.environ', {
                'SPARK_KUBECTL_ARGS': '--kubeconfig /tmp/agent-kubeconfig',
                'SPARK_DOCKER_SOCK': 'unix:///host/run/docker.sock'}):
            result = self.agent.collect_status('master', runner=run,
                identity=lambda: (host_id, 'boot-1', 'spark'), now=lambda: 1234)
        self.assertTrue(result['inventory_complete'])
        kubectl_calls = [call for call in calls if call[0] == 'kubectl']
        self.assertEqual(len(kubectl_calls), 2)
        for call in kubectl_calls:
            self.assertEqual(call[:3], ['kubectl', '--kubeconfig', '/tmp/agent-kubeconfig'])
        docker_call = next(call for call in calls if call[0] == 'docker')
        self.assertEqual(docker_call[:3], ['docker', '--host', 'unix:///host/run/docker.sock'])

    def test_container_probe_rejects_remote_docker_socket(self):
        with mock.patch.dict('os.environ', {'SPARK_DOCKER_SOCK': 'tcp://remote:2375'}):
            with self.assertRaises(ValueError):
                self.collect(lambda _: self.fail('invalid socket must not reach runner'))

    def test_inventory_only_contains_allowed_metadata(self):
        calls = []
        def run(argv):
            calls.append(argv)
            if argv[0] == 'docker':
                return json.dumps({'ID': 'abc', 'Names': 'model-gateway', 'State': 'running',
                                   'Secrets': 'must-not-escape'}) + '\n'
            return 'lework-prod\tworker-1\tRunning\n'
        result = self.collect(run)
        self.assertEqual(result['mode'], 'read_only')
        self.assertEqual(result['host_id'], hashlib.sha256(b'lework-spark:machine-1').hexdigest())
        self.assertEqual(result['observed_at'], 1234)
        self.assertEqual([x['id'] for x in result['components']], ['lework-prod/worker-1', 'abc'])
        self.assertNotIn('must-not-escape', json.dumps(result))
        self.assertTrue(all(not x['registered'] for x in result['components']))
        self.assertIn('spec.nodeName=master', calls[0])
        self.assertNotIn('exec', [part for call in calls for part in call])
        self.assertIn('execution_disabled', [x['code'] for x in result['checks']])

    def test_probe_failure_is_unknown_not_empty_success(self):
        def fail(_):
            raise RuntimeError('secret command output')
        result = self.collect(fail)
        self.assertEqual(len([c for c in result['checks'] if c['code'].endswith('_unavailable')]), 4)
        self.assertNotIn('secret command output', json.dumps(result))
        self.assertFalse(result['inventory_complete'])

    def test_malformed_probe_and_oversized_inventory_are_rejected(self):
        for value in ['garbage', json.dumps({'Names': 'missing id'})]:
            result = self.collect(lambda _: value)
            self.assertFalse(result['inventory_complete'])
        with self.assertRaises(ValueError):
            self.agent.collect_status('../bad-node')

    def test_node_identity_is_verified_and_docker_is_local(self):
        for node_id in ['', 'remote-machine']:
            result = self.agent.collect_status('does-not-exist', runner=lambda _: node_id,
                identity=lambda: ('host-1', 'boot-1', 'spark'), now=lambda: 1234)
            self.assertFalse(result['inventory_complete'])
            self.assertIn('kubernetes_unavailable', [c['code'] for c in result['checks']])
        calls = []
        self.collect(lambda argv: calls.append(argv) or '')
        docker_call = next(c for c in calls if c[0] == 'docker')
        self.assertIn('unix:///var/run/docker.sock', docker_call)

    def test_empty_success_does_not_enable_shutdown(self):
        result = self.collect(lambda _: '')
        self.assertTrue(result['inventory_complete'])
        self.assertIn('execution_disabled', [x['code'] for x in result['checks']])

    def test_unix_http_get_and_post_does_not_execute(self):
        with tempfile.TemporaryDirectory() as d:
            path = str(Path(d) / 'agent.sock')
            snapshot = self.collect(lambda _: '')
            server = self.agent.make_server(path, lambda: snapshot)
            self.assertEqual(Path(path).stat().st_mode & 0o777, 0o660)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                for method, route, expected in [('GET', '/status', '200'), ('POST', '/shutdown', '405'),
                                                 ('GET', '/status?host=elsewhere', '404')]:
                    with socket.socket(socket.AF_UNIX) as client:
                        client.settimeout(2)
                        client.connect(path)
                        client.sendall(f'{method} {route} HTTP/1.0\r\nHost: local\r\n\r\n'.encode())
                        body = b''
                        while chunk := client.recv(4096):
                            body += chunk
                        self.assertIn(expected, body.decode().splitlines()[0])
                with self.assertRaises(FileExistsError):
                    self.agent.make_server(path, lambda: snapshot)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()


if __name__ == '__main__':
    unittest.main()
