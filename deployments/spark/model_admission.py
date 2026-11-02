"""Model-side admission barrier; observe requests via Unix socket, never expose control over TCP."""
import json
import fcntl
import http.client
from http.server import BaseHTTPRequestHandler
import os
from pathlib import Path
import socketserver
import stat
import threading
import time


class AdmissionGate:
    def __init__(self, read_state, host_id, boot_id, model_id, container_id='', port=0):
        self.read_state = read_state
        self.host_id, self.boot_id, self.model_id = host_id, boot_id, model_id
        self.container_id, self.port = container_id, port
        self.lock = threading.Lock()
        self.in_flight = 0

    def _state(self):
        state = self.read_state()
        if type(state.get('active')) is not bool or type(state.get('revision')) is not int or state['revision'] < 1:
            raise ValueError('invalid maintenance state')
        return state

    def enter(self):
        with self.lock:
            try:
                if self._state()['active']:
                    return False
            except (ValueError, OSError, TypeError):
                return False
            self.in_flight += 1
            return True

    def leave(self):
        with self.lock:
            self.in_flight -= 1

    def snapshot(self):
        with self.lock:
            result = {'protocol_version': 1, 'host_id': self.host_id, 'boot_id': self.boot_id,
                      'model_id': self.model_id, 'observed_at': int(time.time()),
                      'container_id': self.container_id,
                      'known': False, 'maintenance_active': True, 'maintenance_revision': 0,
                      'in_flight': self.in_flight}
            try:
                state = self._state()
                result.update(known=True, maintenance_active=state['active'], maintenance_revision=state['revision'])
            except (ValueError, OSError, TypeError):
                pass
            return result


class AdmissionMiddleware:
    def __init__(self, app, gate):
        self.app, self.gate = app, gate

    async def __call__(self, scope, receive, send):
        if scope['type'] not in ('http', 'websocket'):
            return await self.app(scope, receive, send)
        if scope['type'] == 'http' and scope['method'] == 'GET' and scope['path'] in ('/health', '/v1/models', '/metrics'):
            return await self.app(scope, receive, send)
        if not self.gate.enter():
            if scope['type'] == 'websocket':
                await send({'type': 'websocket.close', 'code': 1013})
                return
            body = b'{"error":{"message":"Model is under maintenance","type":"model_maintenance"}}'
            await send({'type': 'http.response.start', 'status': 503, 'headers': [
                (b'content-type', b'application/json'), (b'content-length', str(len(body)).encode())]})
            await send({'type': 'http.response.body', 'body': body})
            return
        try:
            await self.app(scope, receive, send)
        finally:
            self.gate.leave()


def serve_observation(path, gate):
    # 运维提供独占目录；持有进程锁后才能清理上次异常退出遗留的 socket。
    path = Path(path)
    lock_fd = os.open(str(path) + '.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(lock_fd).st_mode): raise ValueError('invalid admission lock')
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if os.path.lexists(path):
            if not stat.S_ISSOCK(path.lstat().st_mode): raise ValueError('invalid admission socket')
            path.unlink()
    except BaseException:
        os.close(lock_fd)
        raise
    class Server(socketserver.UnixStreamServer):
        def get_request(self):
            conn, address = super().get_request()
            conn.settimeout(3)
            return conn, address
        def server_close(self):
            super().server_close()
            if getattr(self, '_closed', False): return
            self._closed = True
            try: path.unlink(missing_ok=True)
            finally: os.close(lock_fd)
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path in ('/health', '/models', '/metrics'):
                # 仅转发固定只读探针到本容器回环地址，不接受调用者提供 URL 或查询参数。
                conn = http.client.HTTPConnection('127.0.0.1', gate.port, timeout=2)
                try:
                    conn.request('GET', '/v1/models' if self.path == '/models' else self.path)
                    response = conn.getresponse(); body = response.read(1024 * 1024 + 1)
                    if response.status != 200 or len(body) > 1024 * 1024:
                        raise ValueError('probe unavailable')
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/plain' if self.path == '/metrics' else 'application/json')
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers();self.wfile.write(body)
                except (OSError, ValueError, http.client.HTTPException):
                    self.send_error(503, 'model probe unavailable')
                finally:
                    conn.close()
                return
            if self.path != '/admission':
                self.send_error(404)
                return
            body = json.dumps(gate.snapshot()).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        def log_message(self, *_):
            pass
    old = os.umask(0o117)
    try:
        server = Server(str(path), Handler)
    except BaseException:
        # UnixStreamServer 绑定失败时自身会调用 server_close。
        raise
    finally:
        os.umask(old)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server
