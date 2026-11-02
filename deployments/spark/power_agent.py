#!/usr/bin/env python3
"""Spark inventory with optional admission maintenance; no model, container or power actions."""
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


# 容器化后宿主机身份通过只读挂载与环境变量提供，默认值保持原生部署行为。
MACHINE_ID_PATH = '/etc/machine-id'
DEFAULT_DOCKER_SOCK = 'unix:///var/run/docker.sock'


def host_identity():
    machine = Path(os.environ.get('SPARK_MACHINE_ID_PATH', MACHINE_ID_PATH)).read_text().strip()
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    if not machine or not boot:
        raise ValueError('host identity unavailable')
    hostname = os.environ.get('SPARK_HOSTNAME') or socket.gethostname()
    return hashlib.sha256(('lework-spark:' + machine).encode()).hexdigest(), boot, hostname


def kubectl_prefix():
    # 默认走 kubectl 自动发现（kubeconfig 或 Pod 内 ServiceAccount），不接受 shell 展开。
    extra = os.environ.get('SPARK_KUBECTL_ARGS', '').split()
    return ['kubectl', *extra]


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
        check('execution_disabled', '模型启停和系统关机能力尚未启用。'),
        check('model_unconfigured', '未配置可管理的本机模型；模型入口不代表推理进程。'),
        check('maintenance_unavailable', '尚未接入可恢复的任务维护与请求排空协议。'),
        check('recovery_unavailable', '服务停止顺序和开机恢复尚未完成验证。'),
    ]
    components = []
    complete = True
    docker_sock = os.environ.get('SPARK_DOCKER_SOCK', DEFAULT_DOCKER_SOCK)
    if not re.fullmatch(r'unix:///[A-Za-z0-9._/-]+', docker_sock):
        raise ValueError('invalid docker socket')
    probes = [
        ('kubernetes', [*kubectl_prefix(), '--request-timeout=3s', 'get', 'pods', '-A',
                        '--field-selector', 'spec.nodeName=' + node,
                        '-o', 'jsonpath={range .items[*]}{.metadata.namespace}{"\\t"}{.metadata.name}{"\\t"}{.status.phase}{"\\n"}{end}']),
        ('docker', ['docker', '--host', docker_sock, 'ps', '-a', '--format', '{{json .}}']),
    ]
    for kind, argv in probes:
        try:
            if kind == 'kubernetes':
                node_machine = runner([*kubectl_prefix(), '--request-timeout=3s', 'get', 'node', node,
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


def make_server(path, collect, maintenance=None, identity=host_identity, model=None):
    if os.path.lexists(path):
        raise FileExistsError('socket path already exists; refusing to replace it')

    class Handler(BaseHTTPRequestHandler):
        def reply(self, status, result):
            payload = json.dumps(result, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(payload)))
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            self.wfile.write(payload)

        def maintenance_status(self):
            host, boot, _ = identity()
            state = maintenance.read()
            if state['host_id'] != host:
                raise ValueError('host mismatch')
            return dict(state, boot_id=boot, observed_at=int(time.time()))

        def do_GET(self):
            try:
                if self.path == '/status':
                    self.reply(200, collect())
                elif self.path == '/maintenance' and maintenance is not None:
                    self.reply(200, self.maintenance_status())
                elif self.path == '/model' and model is not None:
                    self.reply(200, model.observe())
                else:
                    self.reply(404, {'error': 'unsupported'})
            except Exception:
                self.reply(503, {'error': 'status_unavailable'})

        def do_POST(self):
            if maintenance is None or self.path not in ('/maintenance', '/model') or (self.path == '/model' and model is None):
                self.reply(405, {'error': 'read_only'})
                return
            try:
                lengths = self.headers.get_all('Content-Length', [])
                if (len(lengths) != 1 or self.headers.get('Transfer-Encoding') is not None
                        or not lengths[0].isdigit() or not 1 <= int(lengths[0]) <= 2048):
                    raise ValueError('invalid length')
                def unique(pairs):
                    result = {}
                    for key, value in pairs:
                        if key in result:
                            raise ValueError('duplicate field')
                        result[key] = value
                    return result
                body = json.loads(self.rfile.read(int(lengths[0])), object_pairs_hook=unique)
                if self.path == '/model':
                    try:
                        self.reply(200, model.begin(body))
                    except ValueError:
                        self.reply(409, {'error': 'model_conflict'})
                    except Exception:
                        self.reply(503, {'error': 'model_outcome_unknown'})
                    return
                if (not isinstance(body, dict) or set(body) != {
                        'host_id', 'boot_id', 'expected_revision', 'operation_id', 'active'}
                        or type(body['expected_revision']) is not int
                        or not 1 <= body['expected_revision'] < 9007199254740991
                        or type(body['active']) is not bool
                        or not isinstance(body['operation_id'], str)
                        or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', body['operation_id'])):
                    raise ValueError('invalid request')
            except (ValueError, TypeError, OSError):
                self.reply(400, {'error': 'invalid_request'})
                return
            try:
                state = self.maintenance_status()
                if model is not None and not body['active'] and model.observe()['busy']:
                    self.reply(409, {'error': 'model_operation_in_progress'})
                    return
                if (body['host_id'] != state['host_id'] or body['boot_id'] != state['boot_id']
                        or body['expected_revision'] != state['revision']
                        or body['active'] == state['active']
                        or (not body['active'] and body['operation_id'] != state['operation_id'])):
                    self.reply(409, {'error': 'maintenance_conflict'})
                    return
                # StateStore 的 CAS 与 CLI 共用锁；响应丢失时客户端必须重新读取，禁止重试写入。
                maintenance.change(body['expected_revision'], body['operation_id'], body['active'])
                self.reply(200, self.maintenance_status())
            except (ValueError, TypeError, OSError):
                self.reply(503, {'error': 'maintenance_outcome_unknown'})

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
    parser.add_argument('--enable-maintenance', action='store_true', help='opt in to host-owned maintenance writes; requires root')
    parser.add_argument('--enable-model-control', action='store_true', help='opt in to one installed local model; requires maintenance and root-managed config')
    args = parser.parse_args()
    if args.snapshot:
        print(json.dumps(collect_status(args.node), ensure_ascii=False))
        return
    maintenance = None
    if args.enable_maintenance:
        # 固定安装目录加载，不接受调用者指定状态路径，也不自动初始化状态。
        import sys
        sys.path.insert(0, str(Path(__file__).resolve().parent))
        from maintenance_ctl import STATE_PATH, validate_directory
        from maintenance_state import StateStore
        if os.geteuid() != 0:
            raise PermissionError('maintenance writes require root')
        validate_directory()
        maintenance = StateStore(STATE_PATH, host_identity()[0])
        maintenance.read()
    model = None
    if args.enable_model_control:
        if maintenance is None:
            raise ValueError('model control requires maintenance')
        from model_control import DockerModel, JOURNAL_PATH, ModelController, load_config
        config = load_config()
        if config['host_id'] != host_identity()[0]:
            raise ValueError('model host binding mismatch')
        model = ModelController(config, JOURNAL_PATH, maintenance.read, DockerModel(config),
                                host_identity, maintenance_guard=maintenance._locked)
    server = make_server(args.socket, lambda: collect_status(args.node), maintenance=maintenance, model=model)
    try:
        server.serve_forever()
    finally:
        server.server_close()
        Path(args.socket).unlink(missing_ok=True)


if __name__ == '__main__':
    main()
