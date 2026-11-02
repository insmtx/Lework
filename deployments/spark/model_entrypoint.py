#!/usr/bin/env python3
"""Pinned vLLM single-API-process wrapper with local admission observation."""
import os
import re
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from maintenance_state import StateStore
from model_admission import AdmissionGate, AdmissionMiddleware, serve_observation
from model_control import read_file


def validate_args(args, environment):
    args = list(args)
    if args and args[0] == 'vllm': args.pop(0)
    if (not args or args[0] != 'serve' or environment.get('VLLM_SERVER_DEV_MODE', '0') not in ('0', '')
            or environment.get('VLLM_RUST_FRONTEND_PATH')):
        raise ValueError('only production vllm serve is supported')
    # 多 API 进程会使请求计数不完整，因此固定为单进程；模型内 GPU 并行不由此参数控制。
    forbidden = ('--api-server-count', '--data-parallel', '--headless', '--config', '--nnodes', '--node-rank')
    if any(any(a.startswith(flag) for flag in forbidden) for a in args):
        raise ValueError('API process count is installation controlled')
    return args + ['--api-server-count', '1']


def install_gate(api_module, gate, on_build=None):
    build = api_module.build_app
    built = False
    def build_app(*args, **kwargs):
        nonlocal built
        if built: raise ValueError('multiple API applications are unsupported')
        built = True
        application = api_module.FastAPI
        created = []
        # vLLM 的 bootstrap 可能提前构建中间件栈，必须在应用构造时安装。
        class GatedApplication(application):
            def __init__(self, *app_args, **app_kwargs):
                if created: raise ValueError('multiple API applications are unsupported')
                super().__init__(*app_args, **app_kwargs)
                self.add_middleware(AdmissionMiddleware, gate=gate)
                created.append(self)
        api_module.FastAPI = GatedApplication
        try:
            app = build(*args, **kwargs)
        finally:
            api_module.FastAPI = application
        if len(created) != 1 or app is not created[0]:
            raise ValueError('API application construction bypassed admission')
        if on_build is not None: on_build()
        return app
    api_module.build_app = build_app


def main():
    args = validate_args(sys.argv[1:], os.environ)
    config = read_file('/etc/leros-spark/model-admission.json')
    if (not isinstance(config, dict) or set(config) != {'host_id', 'model_id', 'container_id', 'container_port'}
            or not all(isinstance(config[k], str) and 0 < len(config[k]) <= 512 for k in ('host_id', 'model_id', 'container_id'))
            or not re.fullmatch(r'[a-f0-9]{64}', config['container_id'])
            or type(config['container_port']) is not int or not 1 <= config['container_port'] <= 65535):
        raise ValueError('invalid admission binding')
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    store = StateStore('/run/leros-spark-state/maintenance.json', config['host_id'])
    store.read()  # 缺配置时启动失败，不创建一个绕过维护的推理服务。
    gate = AdmissionGate(store.read, config['host_id'], boot, config['model_id'], config['container_id'], config['container_port'])
    servers = []
    try:
        import vllm.entrypoints.openai.api_server as api
        install_gate(api, gate, lambda: servers.append(serve_observation('/run/leros-model/admission.sock', gate)))
        from vllm.entrypoints.cli.main import main as serve
        sys.argv = ['vllm', *args]
        serve()
    finally:
        for server in servers:
            server.shutdown();server.server_close()


if __name__ == '__main__': main()
