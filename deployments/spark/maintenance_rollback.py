#!/usr/bin/env python3
"""Serialize a prepared consumer rollback with maintenance state changes."""
import json
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from maintenance_ctl import STATE_PATH, local_host_id, validate_directory
from maintenance_state import StateStore


def run_rollback(store, change_consumers):
    # 同一把锁覆盖状态检查、配置删除和消费者滚动更新完成。
    with store._locked():
        if store.read()['active']:
            raise ValueError('active maintenance cannot be removed')
        return change_consumers()


def main():
    try:
        if os.geteuid() != 0:
            raise PermissionError('root required')
        validate_directory()
        script = Path(__file__).resolve().parent / 'rollback-consumers.sh'
        if not script.is_file():
            raise ValueError('prepared rollback script missing')
        store = StateStore(STATE_PATH, local_host_id())
        return run_rollback(store, lambda: subprocess.run(
            ['runuser', '-u', 'lework', '--', 'bash', str(script), '--apply'], check=False).returncode)
    except (OSError, ValueError, TypeError):
        print(json.dumps({'error': 'maintenance_rollback_failed',
                          'next_step': 'Inspect actual consumer configuration and state before retrying.'}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
