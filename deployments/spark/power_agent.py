#!/usr/bin/env python3
"""Read-only Spark inventory. No model, container, or power mutation commands."""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import subprocess
import tempfile
import time

MAX_BYTES = 1024 * 1024
MAX_COMPONENTS = 256


def host_identity():
    machine = Path('/etc/machine-id').read_text().strip()
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    if not machine or not boot:
        raise ValueError('host identity unavailable')
    return hashlib.sha256(('lework-spark:' + machine).encode()).hexdigest(), boot, socket.gethostname()


def run_probe(argv):
    # 固定程序、无 shell；不把探测器日志或潜在敏感错误返回客户端。
    with tempfile.TemporaryFile() as output:
        subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=output,
                       stderr=subprocess.DEVNULL, timeout=4, check=True)
        output.seek(0)
        data = output.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ValueError('probe response too large')
    return data.decode('utf-8')


def check(code, message):
    return {'code': code, 'message': message}


def collect_status(node, runner=run_probe, identity=host_identity, now=time.time):
    if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?', node):
        raise ValueError('invalid Kubernetes node name')
    host, boot, hostname = identity()
    checks = [
        check('execution_disabled', '当前管理服务仅支持只读预检，尚未启用执行能力。'),
        check('model_unconfigured', '未配置可管理的本机模型；模型入口不代表推理进程。'),
        check('maintenance_unavailable', '尚未接入可恢复的任务维护与请求排空协议。'),
        check('recovery_unavailable', '服务停止顺序和开机恢复尚未完成验证。'),
    ]
    components = []
    complete = True
    probes = [
        ('kubernetes', ['kubectl', '--request-timeout=3s', 'get', 'pods', '-A',
                        '--field-selector', 'spec.nodeName=' + node,
                        '-o', 'jsonpath={range .items[*]}{.metadata.namespace}{"\\t"}{.metadata.name}{"\\t"}{.status.phase}{"\\n"}{end}']),
        ('docker', ['docker', '--host', 'unix:///var/run/docker.sock', 'ps', '-a', '--format', '{{json .}}']),
    ]
    for kind, argv in probes:
        try:
            if kind == 'kubernetes':
                node_machine = runner(['kubectl', '--request-timeout=3s', 'get', 'node', node,
                                       '-o', 'jsonpath={.status.nodeInfo.machineID}']).strip()
                node_host = hashlib.sha256(('lework-spark:' + node_machine).encode()).hexdigest()
                if not node_machine or node_host != host:
                    raise ValueError('node does not match local machine')
            raw = runner(argv)
            if len(raw.encode()) > MAX_BYTES:
                raise ValueError('probe response too large')
            items = []
            for line in raw.splitlines():
                if kind == 'docker':
                    obj = json.loads(line)
                    item_id, name, state = obj['ID'], obj['Names'], obj['State']
                else:
                    namespace, name, state = line.split('\t')
                    if namespace == 'kube-system':
                        continue
                    item_id = namespace + '/' + name
                if not all(isinstance(v, str) and 0 < len(v) <= 512 for v in (item_id, name, state)):
                    raise ValueError('invalid component')
                items.append({'kind': kind, 'id': item_id, 'name': name,
                              'state': state, 'registered': False})
                if len(items) + len(components) > MAX_COMPONENTS:
                    raise ValueError('too many components')
            components.extend(items)
        except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError):
            complete = False
            checks.append(check(kind + '_unavailable', kind + ' 工作负载状态无法确认。'))
    if components:
        checks.append(check('unregistered_workloads', '发现尚未登记停止与恢复策略的业务工作负载。'))
    return {'protocol_version': 1, 'host_id': host, 'boot_id': boot, 'hostname': hostname,
            'observed_at': int(now()), 'mode': 'read_only', 'model_state': 'unconfigured',
            'inventory_complete': complete, 'components': components, 'checks': checks}


class UnixServer(socketserver.UnixStreamServer):
    # 串行处理且每连接限时，避免无限创建采集线程。
    def get_request(self):
        conn, address = super().get_request()
        conn.settimeout(3)
        return conn, address


def make_server(path, collect):
    if os.path.lexists(path):
        raise FileExistsError('socket path already exists; refusing to replace it')

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != '/status':
                self.send_error(404)
                return
            try:
                payload = json.dumps(collect(), ensure_ascii=False).encode()
                self.send_response(200)
            except Exception:
                payload = b'{"error":"inventory_unavailable"}'
                self.send_response(503)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(payload)))
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            self.wfile.write(payload)

        def do_POST(self):
            self.send_error(405, 'Read-only agent')

        def log_message(self, *_args):
            pass

    old_umask = os.umask(0o117)
    try:
        return UnixServer(path, Handler)
    finally:
        os.umask(old_umask)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--node', required=True, help='local Kubernetes node name, configured by operations')
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--snapshot', action='store_true')
    mode.add_argument('--socket', help='Unix socket in an operations-owned directory')
    args = parser.parse_args()
    if args.snapshot:
        print(json.dumps(collect_status(args.node), ensure_ascii=False))
        return
    server = make_server(args.socket, lambda: collect_status(args.node))
    try:
        server.serve_forever()
    finally:
        server.server_close()
        Path(args.socket).unlink(missing_ok=True)


if __name__ == '__main__':
    main()
