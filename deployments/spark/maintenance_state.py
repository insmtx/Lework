"""Host-owned persistent admission state primitives; no public endpoint or service stop commands."""
import contextlib
import fcntl
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import time


class StateStore:
    def __init__(self, path, host_id):
        self.path = Path(path)
        if not self.path.is_absolute() or not isinstance(host_id, str) or not host_id:
            raise ValueError('invalid maintenance installation binding')
        self.host_id = host_id

    @contextlib.contextmanager
    def _locked(self):
        # 状态目录由运维创建并控制；拒绝跟随 lock 符号链接。
        fd = os.open(str(self.path) + '.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            if not stat.S_ISREG(os.fstat(fd).st_mode):
                raise ValueError('invalid maintenance lock')
            fcntl.flock(fd, fcntl.LOCK_EX)
            yield
        finally:
            os.close(fd)

    def read(self):
        fd = os.open(self.path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd) as handle:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                raise ValueError('invalid maintenance state')
            raw = handle.read(4097)
        if len(raw.encode()) > 4096:
            raise ValueError('maintenance state too large')
        state = json.loads(raw)
        if (not isinstance(state, dict) or type(state.get('version')) is not int or state.get('version') != 1
                or state.get('host_id') != self.host_id or type(state.get('active')) is not bool
                or type(state.get('revision')) is not int or not 1 <= state['revision'] <= 9007199254740991
                or not isinstance(state.get('operation_id'), str)
                or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', state['operation_id'])
                or type(state.get('updated_at')) is not int or not 0 < state['updated_at'] <= time.time() + 5):
            raise ValueError('invalid maintenance state')
        return state

    def initialize(self):
        with self._locked():
            if os.path.lexists(self.path):
                return self.read()
            state = {'version': 1, 'host_id': self.host_id, 'active': False,
                     'revision': 1, 'operation_id': 'initialized', 'updated_at': int(time.time())}
            self._write(state)
            return state

    def change(self, expected_revision, operation_id, active):
        if (type(expected_revision) is not int or type(active) is not bool
                or not isinstance(operation_id, str)
                or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', operation_id)):
            raise ValueError('invalid maintenance transition')
        with self._locked():
            state = self.read()
            if state['revision'] != expected_revision or expected_revision >= 9007199254740991:
                raise ValueError('maintenance revision conflict')
            if not active and state['active'] and state['operation_id'] != operation_id:
                raise ValueError('maintenance operation conflict')
            state.update(active=active, revision=expected_revision + 1,
                         operation_id=operation_id, updated_at=int(time.time()))
            self._write(state)
            return state

    def _write(self, state):
        fd, name = tempfile.mkstemp(prefix='.maintenance-', dir=self.path.parent)
        try:
            with os.fdopen(fd, 'w') as handle:
                os.fchmod(handle.fileno(), 0o640)
                json.dump(state, handle, separators=(',', ':'))
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(name, self.path)
            directory = os.open(self.path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        finally:
            if os.path.exists(name):
                os.unlink(name)
