import asyncio
import importlib.util
from pathlib import Path
import tempfile
import socket
import json
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ModelAdmissionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('model_admission', ROOT / 'model_admission.py')
        cls.m = importlib.util.module_from_spec(spec); spec.loader.exec_module(cls.m)

    def test_inflight_stream_is_counted_until_complete_and_new_requests_rejected(self):
        state = {'active': False, 'revision': 9}
        gate = self.m.AdmissionGate(lambda: dict(state), 'host', 'boot', 'qwen')
        async def scenario():
            entered, finish = asyncio.Event(), asyncio.Event()
            async def app(scope, receive, send):
                entered.set(); await finish.wait()
            wrapped = self.m.AdmissionMiddleware(app, gate=gate)
            received = []
            async def send(msg): received.append(msg)
            task = asyncio.create_task(wrapped({'type': 'http', 'method': 'POST', 'path': '/v1/chat/completions'}, None, send))
            await entered.wait()
            state.update(active=True, revision=10)
            self.assertEqual(gate.snapshot()['in_flight'], 1)
            await wrapped({'type': 'http', 'method': 'POST', 'path': '/v1/completions'}, None, send)
            self.assertEqual(received[0]['status'], 503)
            self.assertEqual(gate.snapshot()['in_flight'], 1)
            finish.set(); await task
            self.assertEqual(gate.snapshot()['in_flight'], 0)
            self.assertEqual(gate.snapshot()['maintenance_revision'], 10)
        asyncio.run(scenario())

    def test_unreadable_gate_fails_closed_but_health_can_be_observed(self):
        def fail(): raise ValueError('secret')
        gate = self.m.AdmissionGate(fail, 'host', 'boot', 'qwen')
        calls = []
        async def app(scope, receive, send): calls.append(scope['path'])
        messages = []
        async def send(message): messages.append(message)
        wrapped = self.m.AdmissionMiddleware(app, gate=gate)
        async def scenario():
            await wrapped({'type':'http','method':'POST','path':'/v1/chat/completions'},None,send)
            await wrapped({'type':'http','method':'GET','path':'/health'},None,send)
        asyncio.run(scenario())
        self.assertEqual(calls, ['/health'])
        self.assertEqual(messages[0]['status'], 503)
        self.assertFalse(gate.snapshot()['known'])
        self.assertNotIn('secret', str(messages))

    def test_cancellation_releases_inflight_and_only_get_health_is_exempt(self):
        state={'active':False,'revision':1}
        gate=self.m.AdmissionGate(lambda:state,'h','b','m')
        async def app(scope, receive, send): raise asyncio.CancelledError()
        async def send(_): pass
        async def scenario():
            with self.assertRaises(asyncio.CancelledError):
                await self.m.AdmissionMiddleware(app,gate=gate)({'type':'http','method':'POST','path':'/health'},None,send)
        asyncio.run(scenario())
        self.assertEqual(gate.snapshot()['in_flight'],0)
        state['active']=True
        self.assertFalse(gate.enter())

    def test_unix_observation_is_read_only_and_restarts_without_stale_socket(self):
        gate=self.m.AdmissionGate(lambda:{'active':True,'revision':9},'host','boot','qwen')
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'admission.sock'
            for _ in range(2):
                server=self.m.serve_observation(path,gate)
                try:
                    with self.assertRaises(BlockingIOError):self.m.serve_observation(path,gate)
                    with socket.socket(socket.AF_UNIX) as conn:
                        conn.settimeout(2);conn.connect(str(path))
                        conn.sendall(b'GET /admission HTTP/1.0\r\n\r\n')
                        body=b''
                        while chunk:=conn.recv(4096):body+=chunk
                        self.assertIn(b'200',body.splitlines()[0])
                        self.assertEqual(json.loads(body.split(b'\r\n\r\n')[1])['in_flight'],0)
                finally:
                    server.shutdown();server.server_close()
                self.assertFalse(path.exists())

    def test_fixed_read_only_probes_use_model_loopback_through_unix_socket(self):
        calls=[]
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                calls.append(self.path)
                body=b'{"data":[{"id":"qwen"}]}' if self.path=='/v1/models' else b'{}'
                self.send_response(200);self.send_header('Content-Length',str(len(body)))
                self.end_headers();self.wfile.write(body)
            def log_message(self,*_):pass
        upstream=HTTPServer(('127.0.0.1',0),Handler)
        thread=threading.Thread(target=upstream.serve_forever,daemon=True);thread.start()
        try:
            gate=self.m.AdmissionGate(lambda:{'active':True,'revision':9},'h','b','qwen','a'*64,upstream.server_port)
            with tempfile.TemporaryDirectory() as d:
                server=self.m.serve_observation(Path(d)/'sock',gate)
                try:
                    for path,expected in [('/health',200),('/models',200),('/metrics',200),('/health?url=remote',404)]:
                        with socket.socket(socket.AF_UNIX) as conn:
                            conn.settimeout(3);conn.connect(server.server_address)
                            conn.sendall(('GET '+path+' HTTP/1.0\r\n\r\n').encode())
                            body=b''
                            while chunk:=conn.recv(4096):body+=chunk
                            self.assertEqual(int(body.split(b' ')[1]),expected)
                    self.assertEqual(calls,['/health','/v1/models','/metrics'])
                finally:server.shutdown();server.server_close()
        finally:upstream.shutdown();upstream.server_close();thread.join()


if __name__ == '__main__': unittest.main()
