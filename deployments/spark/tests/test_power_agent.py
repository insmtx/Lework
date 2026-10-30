import hashlib
import importlib.util
import json
from pathlib import Path
import socket
import tempfile
import threading
import unittest

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
