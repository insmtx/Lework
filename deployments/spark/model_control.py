"""Opt-in single Docker model controller. No container creation, deletion, kill, or host power API."""
import contextlib
import fcntl
import http.client
import json
import math
import os
from pathlib import Path
import re
import socket
import stat
import tempfile
import threading
import time

CONFIG_PATH = Path('/etc/leros-spark/model.json')
JOURNAL_PATH = Path('/var/lib/leros-spark/model-operation.json')
ADMISSION_SOCKET = '/run/leros-model/admission.sock'
MAX_BYTES = 1024 * 1024


def validate_config(config):
    keys = {'version', 'host_id', 'model_id', 'label', 'scope', 'container_id', 'image_id', 'container_port', 'served_model'}
    if (not isinstance(config, dict) or set(config) != keys or type(config['version']) is not int
            or config['version'] != 1 or config['scope'] != 'local'
            or not all(isinstance(config[k], str) and 0 < len(config[k]) <= 512 for k in keys - {'version', 'container_port'})
            or type(config['container_port']) is not int or not 1 <= config['container_port'] <= 65535
            or not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,63}', config['model_id'])
            or not re.fullmatch(r'[a-f0-9]{64}', config['container_id'])
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', config['image_id'])):
        raise ValueError('invalid model configuration')
    return dict(config)


def read_file(path, limit=4096):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o022:
            raise ValueError('invalid managed file')
        data = stream.read(limit + 1)
    if len(data.encode()) > limit:
        raise ValueError('managed file too large')
    def unique(pairs):
        result = {}
        for k, v in pairs:
            if k in result: raise ValueError('duplicate field')
            result[k] = v
        return result
    return json.loads(data, object_pairs_hook=unique)


def load_config():
    for path in [CONFIG_PATH.parent, CONFIG_PATH]:
        info = path.lstat()
        if info.st_uid != 0 or info.st_mode & 0o022 or stat.S_ISLNK(info.st_mode):
            raise ValueError('model configuration must be root managed')
    return validate_config(read_file(CONFIG_PATH))


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path, timeout=3):
        super().__init__('localhost', timeout=timeout)
        self.path = path
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


def request_json(connection, method, path, expected=(200,)):
    try:
        connection.request(method, path)
        response = connection.getresponse()
        raw = response.read(MAX_BYTES + 1)
        if response.status not in expected or len(raw) > MAX_BYTES:
            raise ValueError('model observation unavailable')
        return json.loads(raw) if raw else None
    finally:
        connection.close()


class DockerModel:
    def __init__(self, config):
        self.config = validate_config(config)
        self.target = '/containers/' + self.config['container_id']

    def _inspect(self):
        obj = request_json(UnixHTTP('/var/run/docker.sock'), 'GET', self.target + '/json')
        state = obj['State']
        if (obj['Id'] != self.config['container_id'] or obj['Image'] != self.config['image_id']
                or obj['Config'].get('Labels', {}).get('io.lework.model-control') != 'v1'
                or obj['Config'].get('StopSignal') not in (None, '', 'SIGTERM', '15')
                or state['Paused'] or state['Restarting'] or state['Dead']
                or type(state['Running']) is not bool or type(state['ExitCode']) is not int
                or type(state['OOMKilled']) is not bool
                or state['Status'] not in ('running', 'exited', 'created')):
            raise ValueError('model identity or state mismatch')
        return obj

    def inspect(self):
        state = self._inspect()['State']
        return {'running': state['Running'], 'status': state['Status'],
                'exit_code': state['ExitCode'], 'oom_killed': state['OOMKilled']}

    def health(self):
        try:
            self.inspect()
            request_json(UnixHTTP(ADMISSION_SOCKET), 'GET', '/health')
            obj = request_json(UnixHTTP(ADMISSION_SOCKET), 'GET', '/models')
            return isinstance(obj, dict) and any(m.get('id') == self.config['served_model'] for m in obj.get('data', []) if isinstance(m, dict))
        except (OSError, ValueError, KeyError, TypeError, http.client.HTTPException):
            return False

    def admission(self):
        return request_json(UnixHTTP(ADMISSION_SOCKET), 'GET', '/admission')

    def idle(self):
        # HTTP 客户端断开后引擎取消可能仍在途，不能只依赖 ASGI 请求计数。
        conn = UnixHTTP(ADMISSION_SOCKET)
        try:
            conn.request('GET', '/metrics')
            response = conn.getresponse(); raw = response.read(MAX_BYTES + 1)
            if response.status != 200 or len(raw) > MAX_BYTES:
                return False
            seen = set()
            for line in raw.decode().splitlines():
                match = re.fullmatch(r'vllm:num_requests_(running|waiting)(?:\{[^\n]*\})?\s+([0-9.eE+-]+)(?:\s+[0-9]+)?', line)
                if line.startswith(('vllm:num_requests_running', 'vllm:num_requests_waiting')) and not match:
                    return False
                if match:
                    value = float(match[2])
                    if not math.isfinite(value) or value != 0: return False
                    seen.add(match[1])
            return seen == {'running', 'waiting'}
        except (OSError, ValueError, http.client.HTTPException):
            return False
        finally:
            conn.close()

    def start(self):
        self.inspect()
        request_json(UnixHTTP('/var/run/docker.sock', timeout=30), 'POST', self.target + '/start', (204, 304))

    def stop(self):
        self.inspect()
        # t=-1 等待正常退出，Docker 不在超时后升级为 SIGKILL；HTTP 超时只表示结果未知。
        request_json(UnixHTTP('/var/run/docker.sock', timeout=305), 'POST', self.target + '/stop?t=-1', (204, 304))


class ModelController:
    def __init__(self, config, path, read_maintenance, driver, identity, now=time.time,
                 sleep=time.sleep, launch=None, attempts=150, maintenance_guard=contextlib.nullcontext):
        self.config = validate_config(config)
        self.path, self.read_maintenance, self.driver, self.identity = Path(path), read_maintenance, driver, identity
        self.now, self.sleep, self.attempts = now, sleep, attempts
        self.maintenance_guard = maintenance_guard
        self.lock = threading.RLock()
        self.launch = launch or (lambda fn: threading.Thread(target=fn, daemon=True).start())
        if not self.path.exists():
            fd = self._file_lock()
            try:
                if not self.path.exists():
                    self._write({'revision': 1, 'operation_id': 'initialized', 'action': 'none',
                                 'phase': 'idle', 'error_code': '', 'busy': False})
            finally: os.close(fd)
        self._read()

    def _file_lock(self):
        fd = os.open(str(self.path) + '.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        try:
            if not stat.S_ISREG(os.fstat(fd).st_mode): raise ValueError('invalid model lock')
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return fd
        except BlockingIOError:
            os.close(fd)
            raise ValueError('model operation busy') from None
        except BaseException:
            os.close(fd)
            raise

    def _read(self):
        record = read_file(self.path)
        if (record.get('host_id') != self.config['host_id'] or record.get('container_id') != self.config['container_id']
                or type(record.get('revision')) is not int or not 1 <= record['revision'] <= 9007199254740991
                or type(record.get('busy')) is not bool
                or record.get('action') not in ('none', 'start', 'stop')
                or record.get('phase') not in ('idle', 'starting', 'draining', 'stopping', 'error')
                or not isinstance(record.get('operation_id'), str)
                or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', record['operation_id'])
                or not isinstance(record.get('error_code'), str)):
            raise ValueError('invalid model operation record')
        return record

    def _write(self, record):
        record = dict(record, host_id=self.config['host_id'], container_id=self.config['container_id'], boot_id=self.identity()[1])
        fd, name = tempfile.mkstemp(prefix='.model-', dir=self.path.parent)
        try:
            with os.fdopen(fd, 'w') as stream:
                os.fchmod(stream.fileno(), 0o640)
                json.dump(record, stream);stream.flush();os.fsync(stream.fileno())
            os.replace(name, self.path)
            fd = os.open(self.path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try: os.fsync(fd)
            finally: os.close(fd)
        finally:
            if os.path.exists(name): os.unlink(name)

    def _gate(self, revision):
        gate = self.read_maintenance()
        if gate.get('active') is not True or gate.get('revision') != revision:
            raise ValueError('maintenance conflict')
        a = self.driver.admission()
        if (type(a.get('protocol_version')) is not int or a.get('protocol_version') != 1 or a.get('host_id') != self.config['host_id']
                or a.get('container_id') != self.config['container_id']
                or a.get('boot_id') != self.identity()[1] or a.get('model_id') != self.config['model_id']
                or a.get('known') is not True or a.get('maintenance_active') is not True
                or type(a.get('maintenance_revision')) is not int or a['maintenance_revision'] != revision
                or type(a.get('in_flight')) is not int or a['in_flight'] < 0
                or type(a.get('observed_at')) is not int or not -5 <= self.now() - a['observed_at'] <= 5):
            raise ValueError('model admission unconfirmed')
        return a['in_flight']

    def observe(self):
        with self.lock:
            record = self._read()
            host, boot, _ = self.identity()
            if host != self.config['host_id']: raise ValueError('host mismatch')
            result = {'protocol_version': 1, 'host_id': host, 'boot_id': boot,
                      'observed_at': int(self.now()), 'model_id': self.config['model_id'],
                      'label': self.config['label'], 'state': 'unknown',
                      **{k: record[k] for k in ['revision', 'operation_id', 'action', 'error_code', 'busy']}}
            if record['busy']:
                result['state'] = record['phase'] if record['boot_id'] == boot else 'unknown'
                return result
            if record['phase'] == 'error':
                result['state'] = 'error'
                return result
            try:
                physical = self.driver.inspect()
                if not physical['running']:
                    result['state'] = 'stopped'
                elif self.driver.health():
                    # 就绪还须具备本模型观察通道；暂停标志可为 true，表示尚未恢复任务准入。
                    a = self.driver.admission()
                    if (a.get('known') is True and a.get('host_id') == host and a.get('boot_id') == boot
                            and a.get('container_id') == self.config['container_id']
                            and a.get('model_id') == self.config['model_id']
                            and type(a.get('observed_at')) is int and -5 <= self.now() - a['observed_at'] <= 5):
                        result['state'] = 'ready'
            except (OSError, ValueError, KeyError, TypeError, http.client.HTTPException):
                pass
            return result

    def begin(self, request):
        with self.lock:
            if (not isinstance(request, dict) or set(request) != {'host_id','boot_id','expected_revision','maintenance_revision','operation_id','action'}
                    or request['host_id'] != self.config['host_id'] or request['boot_id'] != self.identity()[1]
                    or type(request['expected_revision']) is not int or type(request['maintenance_revision']) is not int
                    or not isinstance(request['operation_id'], str)
                    or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', request['operation_id'])
                    or request['action'] not in ('start','stop')):
                raise ValueError('invalid model request')
            fd = self._file_lock()
            try:
                record = self._read();state = self.observe();gate = self.read_maintenance()
                if (record['busy'] or record['revision'] != request['expected_revision']
                        or record['revision'] >= 9007199254740991 or gate.get('active') is not True
                        or gate.get('revision') != request['maintenance_revision']
                        or state['state'] != ('stopped' if request['action'] == 'start' else 'ready')):
                    raise ValueError('model transition conflict')
                record.update(revision=record['revision']+1, operation_id=request['operation_id'],
                              action=request['action'], busy=True, error_code='',
                              phase='starting' if request['action']=='start' else 'draining')
                self._write(record)
                self.launch(lambda operation_fd=fd: self._work(record, request['maintenance_revision'], operation_fd))
                fd = None
                return self.observe()
            finally:
                if fd is not None: os.close(fd)

    def _work(self, record, revision, fd):
        try:
            with self.maintenance_guard():
                self._perform(record, revision)
        except Exception:
            with self.lock:
                record.update(busy=True, phase='error', error_code='model_outcome_unknown')
                self._write(record)
        finally:
            os.close(fd)

    def _perform(self, record, revision):
        error = 'model_outcome_unknown'
        try:
            deadline = time.monotonic() + 300
            gate = self.read_maintenance()
            if gate.get('active') is not True or gate.get('revision') != revision:
                raise ValueError('maintenance changed')
            if record['action'] == 'start':
                self.driver.start()
                error = 'model_start_timeout'
                for _ in range(self.attempts):
                    if time.monotonic() >= deadline: raise ValueError('startup deadline reached')
                    try:
                        if self.driver.inspect()['running'] and self.driver.health() and self._gate(revision) == 0:
                            break
                    except (OSError, ValueError, KeyError, TypeError, http.client.HTTPException): pass
                    self.sleep(2)
                else: raise ValueError('startup not confirmed')
            else:
                error = 'model_drain_timeout'
                for _ in range(self.attempts):
                    if time.monotonic() >= deadline: raise ValueError('drain deadline reached')
                    if self._gate(revision) == 0 and self.driver.idle(): break
                    self.sleep(2)
                else: raise ValueError('requests remain active')
                with self.lock:
                    record['phase'] = 'stopping';self._write(record)
                error = 'model_stop_unconfirmed'
                self.driver.stop()
                p = self.driver.inspect()
                if p['running'] or p['oom_killed'] or p['exit_code'] != 0:
                    raise ValueError('normal exit not confirmed')
            with self.lock:
                record.update(busy=False, phase='idle', error_code='');self._write(record)
        except Exception:
            with self.lock:
                # Docker HTTP 断开不会取消 daemon 已接受的 stop；未知结果保留维护屏障。
                unresolved = error in ('model_outcome_unknown', 'model_stop_unconfirmed')
                record.update(busy=unresolved, phase='error', error_code=error);self._write(record)
