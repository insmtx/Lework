#!/usr/bin/env python3
"""Local operations-only admission control; never stops services or powers off a host."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat
import sys

# -I 忽略环境与当前目录，只加载运维安装目录内的配套模块。
sys.path.insert(0, str(Path(__file__).resolve().parent))

from maintenance_state import StateStore

STATE_DIR = Path('/var/lib/leros-spark')
STATE_PATH = STATE_DIR / 'maintenance.json'


def local_host_id():
    machine = Path('/etc/machine-id').read_text().strip()
    if not machine:
        raise ValueError('host identity unavailable')
    return hashlib.sha256(('lework-spark:' + machine).encode()).hexdigest()


def validate_directory():
    info = STATE_DIR.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise ValueError('state directory must be owned and writable only by root')


def sync_directory(directory):
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def initialize_once(store):
    # 初始化锁独立于 StateStore 的变更锁，避免嵌套 flock。
    lock = os.open(store.path.parent / '.installation.lock',
                   os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(lock).st_mode):
            raise ValueError('invalid installation lock')
        fcntl.flock(lock, fcntl.LOCK_EX)
        marker = store.path.parent / '.maintenance-initialized'
        if os.path.lexists(marker):
            # 标记保留：状态丢失或安装中断必须人工核对，不能重建成接单状态。
            if not store.path.exists():
                raise ValueError('initialized state is missing')
            return store.read()
        fd = os.open(marker, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        try:
            os.write(fd, b'1\n')
            os.fsync(fd)
        finally:
            os.close(fd)
        sync_directory(store.path.parent)
        return store.initialize()
    finally:
        os.close(lock)


def operate(path, host_id, action, expected_revision=None, operation_id=None):
    store = StateStore(path, host_id)
    if action == 'status':
        return store.read()
    if action == 'init':
        written = initialize_once(store)
    elif action in ('pause', 'resume'):
        written = store.change(expected_revision, operation_id, action == 'pause')
    else:
        raise ValueError('unsupported operation')
    observed = store.read()
    if observed != written:
        raise ValueError('state changed before confirmation')
    return observed


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    commands.add_parser('init', help='initialize once before configuring consumers')
    commands.add_parser('status', help='read local state; does not confirm consumer admission')
    for action in ('pause', 'resume'):
        command = commands.add_parser(action)
        command.add_argument('--expected-revision', type=int, required=True)
        command.add_argument('--operation-id', required=True)
    args = parser.parse_args(argv)
    attempted = False
    try:
        if os.geteuid() != 0:
            raise PermissionError('root required')
        validate_directory()
        host_id = local_host_id()
        attempted = True
        state = operate(STATE_PATH, host_id, args.action,
                        getattr(args, 'expected_revision', None), getattr(args, 'operation_id', None))
        print(json.dumps({'maintenance': state, 'consumer_confirmation_required': True,
                          'services_changed': False}))
        return 0
    except (OSError, ValueError, TypeError):
        # 写入失败可能发生在 replace 之后；不得声称原状态保持不变。
        print(json.dumps({'error': 'maintenance_command_failed',
                          'outcome': 'unknown' if attempted else 'not_attempted',
                          'next_step': 'Read status and verify both consumers; do not delete state or initialization marker.'}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
