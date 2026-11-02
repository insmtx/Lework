import json
from pathlib import Path
import socket
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from maintenance_state import StateStore
import power_agent


class MaintenanceAPITests(unittest.TestCase):
    def test_opt_in_transition_cas_and_boot_binding(self):
        with tempfile.TemporaryDirectory() as d:
            store = StateStore(Path(d) / 'state', 'host')
            store.initialize()
            server = power_agent.make_server(str(Path(d) / 'sock'), lambda: {},
                maintenance=store, identity=lambda: ('host', 'boot', 'spark'))
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            def request(method, data=None, path='/maintenance'):
                body = json.dumps(data).encode() if data is not None else b''
                with socket.socket(socket.AF_UNIX) as s:
                    s.connect(server.server_address)
                    s.sendall(f'{method} {path} HTTP/1.0\r\nContent-Length: {len(body)}\r\n\r\n'.encode()+body)
                    chunks = []
                    while chunk := s.recv(4096):
                        chunks.append(chunk)
                raw = b''.join(chunks)
                return int(raw.split(b' ')[1]), json.loads(raw.split(b'\r\n\r\n')[1])
            try:
                code, state = request('GET')
                self.assertEqual(code, 200)
                self.assertEqual(state['revision'], 1)
                good = dict(host_id='host', boot_id='boot', expected_revision=1,
                            operation_id='op-1', active=True)
                for bad in [dict(good, boot_id='old'), dict(good, host_id='foreign'),
                            dict(good, expected_revision=True), dict(good, command='shutdown'),
                            dict(good, active='true')]:
                    self.assertNotEqual(request('POST', bad)[0], 200)
                    self.assertEqual(store.read()['revision'], 1)
                self.assertEqual(request('POST', good)[0], 200)
                self.assertTrue(store.read()['active'])
                self.assertEqual(request('POST', good)[0], 409)
                resume = dict(good, active=False, expected_revision=2)
                self.assertEqual(request('POST', dict(resume, operation_id='other'))[0], 409)
                self.assertEqual(request('POST', resume)[0], 200)
                self.assertFalse(store.read()['active'])
                self.assertEqual(store.read()['revision'], 3)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()
