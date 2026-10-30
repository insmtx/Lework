#!/usr/bin/env python3
"""Install the verified Spark user-port setup with a durable recovery journal."""
import argparse
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

UNIT = 'leros-discovery-wired.service'
INTERFACE = 'enP7s7'
SOURCE = Path(__file__).resolve().parent


def atomic_write(path, text, mode=0o644):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp')
    with temporary.open('w', encoding='utf-8') as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(text)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.replace(path)
    # 恢复记录需要在下一次网络变更之前持久化。
    directory = os.open(str(path.parent), os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


class Host:
    """Keep NetworkManager/systemd operations at an explicit process boundary."""
    def run(self, *args):
        result = subprocess.run(args, capture_output=True, text=True, timeout=90,
                                env={**os.environ, 'LC_ALL': 'C'})
        if result.returncode:
            raise RuntimeError(f'{args[0]}: {result.stderr.strip() or result.stdout.strip()}')
        return result.stdout.strip()

    def profile(self, uid):
        ids = self.run('nmcli', '-g', 'UUID', 'connection', 'show').splitlines()
        if uid not in ids:
            return None
        output = self.run('nmcli', '-t', '--escape', 'no', '-f', 'connection,ipv4,ipv6',
                          'connection', 'show', 'uuid', uid)
        return dict(line.split(':', 1) for line in output.splitlines() if ':' in line)

    def preflight(self, interface):
        if self.run('nmcli', '-g', 'GENERAL.TYPE', 'device', 'show', interface) != 'ethernet':
            raise RuntimeError('指定接口不是以太网')
        for service in ('NetworkManager', 'avahi-daemon', 'leros-discovery'):
            self.run('systemctl', 'is-active', '--quiet', service)
        if self.run('systemctl', 'list-units', '--all', '--plain', '--no-legend',
                    'leros-wired-test.service'):
            raise RuntimeError('请先停止临时有线测试')
        self.run(sys.executable, '-c', 'import dbus')
        addresses = json.loads(self.run('ip', '-j', '-4', 'address', 'show', 'dev', interface))
        reserved = [ipaddress.ip_network(n) for n in ('10.100.216.0/24', '10.100.217.0/24', '10.42.0.0/16')]
        for device in addresses:
            for item in device.get('addr_info', []):
                if any(ipaddress.ip_address(item['local']) in network for network in reserved):
                    raise RuntimeError('拒绝修改双机互联或集群网络')

    def active_uuid(self, interface):
        value = self.run('nmcli', '-g', 'GENERAL.CON-UUID', 'device', 'show', interface)
        return '' if value == '--' else value

    def has_carrier(self, interface):
        try:
            return Path(f'/sys/class/net/{interface}/carrier').read_text().strip() == '1'
        except OSError:
            return False

    def create(self, properties):
        values = [entry for pair in properties.items() for entry in pair]
        self.run('nmcli', 'connection', 'add', 'save', 'yes', *values)

    def autoconnect(self, uid, value):
        self.run('nmcli', 'connection', 'modify', 'uuid', uid, 'connection.autoconnect', value)

    def activate(self, uid, interface):
        self.run('nmcli', '--wait', '60', 'connection', 'up', 'uuid', uid, 'ifname', interface)

    def delete(self, uid):
        self.run('nmcli', '--wait', '15', 'connection', 'delete', 'uuid', uid)

    def enable_service(self):
        self.run('systemctl', 'enable', UNIT)
        self.run('systemctl', 'restart', UNIT)
        self.run('systemctl', 'is-active', '--quiet', UNIT)

    def disable_service(self):
        self.run('systemctl', 'disable', '--now', UNIT)

    def reload(self):
        self.run('systemctl', 'daemon-reload')


class Installer:
    def __init__(self, host, root=Path('/')):
        self.host = host
        self.root = root
        self.state_path = root / 'var/lib/leros-wired/state.json'
        self.unit_path = root / 'etc/systemd/system' / UNIT
        self.config_path = root / 'etc/leros-discovery-wired.conf'

    def load_state(self):
        if not self.state_path.exists():
            return None
        state = json.loads(self.state_path.read_text())
        if state['version'] != 1 or state['interface'] != INTERFACE:
            raise RuntimeError('恢复记录版本或接口不匹配，拒绝操作')
        for key in ('original_uuid', 'managed_uuid'):
            if str(uuid.UUID(state[key])) != state[key]:
                raise RuntimeError('恢复记录 UUID 无效')
        if state['original_uuid'] == state['managed_uuid'] or state['original_autoconnect'] not in ('yes', 'no'):
            raise RuntimeError('恢复记录无效')
        return state

    def save_state(self, state):
        self.state_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.state_path.parent.chmod(0o700)
        atomic_write(self.state_path, json.dumps(state, ensure_ascii=False, indent=2) + '\n', 0o600)

    def settings(self, interface):
        config = dict(line.split('=', 1) for line in
                      (self.root / 'etc/leros-discovery.conf').read_text().splitlines()
                      if line and not line.startswith('#'))
        port = config.get('SERVICE_PORT', '')
        scheme = config.get('SERVICE_SCHEME', '')
        if not port.isascii() or not port.isdigit() or not 1 <= int(port) <= 65535 or scheme not in ('http', 'https'):
            raise RuntimeError('现有后端端口或协议配置无效')
        if config.get('LAN_INTERFACE') == interface:
            raise RuntimeError('现有发现服务已经使用此网口，拒绝重复安装')
        uuid.UUID((self.root / 'var/lib/leros-discovery/id').read_text().strip())
        if not (self.root / 'usr/local/lib/leros-discovery/discovery.py').is_file():
            raise RuntimeError('请先安装局域网发现服务')
        return f'LAN_INTERFACE={interface}\nSERVICE_PORT={int(port)}\nSERVICE_SCHEME={scheme}\n'

    def check(self, interface, original_uuid):
        if interface != INTERFACE:
            raise RuntimeError('本版本仅支持已验证的 enP7s7 用户网口')
        if str(uuid.UUID(original_uuid)) != original_uuid:
            raise RuntimeError('旧配置 UUID 无效')
        self.host.preflight(interface)
        original = self.host.profile(original_uuid)
        if not original or original.get('connection.interface-name') != interface or original.get('connection.type') != '802-3-ethernet':
            raise RuntimeError('旧配置不存在或不属于指定用户网口')
        if original.get('connection.autoconnect') not in ('yes', 'no'):
            raise RuntimeError('无法读取原自动连接设置')
        config = self.settings(interface)
        unit = (SOURCE / UNIT).read_text()
        state = self.load_state()
        if state:
            if state['original_uuid'] != original_uuid:
                raise RuntimeError('已有安装使用另一旧配置，请先 rollback')
            if state['phase'] != 'installed':
                raise RuntimeError('存在未完成的安装或回退，请先执行 rollback')
            managed = self.host.profile(state['managed_uuid'])
            expected = self.properties(state['managed_uuid'], interface)
            if not managed or any(managed.get(k) != v for k, v in expected.items()):
                raise RuntimeError('受管连接已被修改，请检查后 rollback')
            if original['connection.autoconnect'] != 'no':
                raise RuntimeError('旧配置自动连接已被修改，请检查后 rollback')
            if self.config_path.read_text() != config or self.unit_path.read_text() != unit:
                raise RuntimeError('有线发现配置已变化，请先 rollback 后重新安装')
        elif self.unit_path.exists() or self.config_path.exists():
            raise RuntimeError('发现未知来源的有线服务文件，拒绝覆盖')
        active = self.host.active_uuid(interface)
        if active and active not in (original_uuid, state['managed_uuid'] if state else ''):
            raise RuntimeError('用户网口正在使用其他连接，拒绝替换')
        return original, config, unit

    @staticmethod
    def properties(uid, interface):
        return {
            'connection.uuid': uid, 'connection.id': 'leros-direct',
            'connection.type': '802-3-ethernet', 'connection.interface-name': interface,
            'connection.autoconnect': 'yes', 'connection.autoconnect-priority': '999',
            'ipv4.method': 'link-local', 'ipv4.never-default': 'yes',
            'ipv6.method': 'link-local', 'ipv6.never-default': 'yes',
        }

    def install(self, interface, original_uuid):
        original, config, unit = self.check(interface, original_uuid)
        if self.load_state():
            self.host.enable_service()
            return
        state = {
            'version': 1, 'phase': 'prepared', 'interface': interface,
            'original_uuid': original_uuid, 'original_autoconnect': original['connection.autoconnect'],
            'original_profile': original, 'original_active': self.host.active_uuid(interface),
            'managed_uuid': str(uuid.uuid4()),
        }
        self.save_state(state)
        try:
            atomic_write(self.config_path, config)
            atomic_write(self.unit_path, unit)
            self.host.reload()
            self.host.create(self.properties(state['managed_uuid'], interface))
            self.host.autoconnect(original_uuid, 'no')
            self.host.enable_service()
            if self.host.has_carrier(interface):
                self.host.activate(state['managed_uuid'], interface)
            state['phase'] = 'installed'
            self.save_state(state)
        except Exception as error:
            try:
                self.rollback()
            except Exception as recovery_error:
                raise RuntimeError(f'{error}; 自动回退未完成: {recovery_error}。请重试 rollback，恢复记录已保留') from error
            raise

    def rollback(self):
        state = self.load_state()
        if not state:
            return
        interface = state['interface']
        old = state['original_uuid']
        managed = state['managed_uuid']
        original = self.host.profile(old)
        if not original or original.get('connection.interface-name') != interface:
            raise RuntimeError('原连接已被删除或改为其他网口，请人工恢复；记录保留')
        managed_profile = self.host.profile(managed)
        if managed_profile and (managed_profile.get('connection.interface-name') != interface
                                or managed_profile.get('connection.type') != '802-3-ethernet'):
            raise RuntimeError('受管连接已改为其他网口或类型，拒绝删除；恢复记录保留')
        active = self.host.active_uuid(interface)
        state['phase'] = 'rolling-back'
        self.save_state(state)
        # 先恢复旧标志，确保删除新连接失败时仍可在下次执行中重试。
        self.host.autoconnect(old, state['original_autoconnect'])
        if self.unit_path.exists():
            self.host.disable_service()
        if self.host.profile(managed):
            self.host.delete(managed)
        # 停服务/删连接期间用户可能切换配置，恢复前再次核对。
        current = self.host.active_uuid(interface)
        if self.host.has_carrier(interface) and active in ('', managed) and current in ('', managed) and (
                state['original_active'] == old or state['original_autoconnect'] == 'yes'):
            self.host.activate(old, interface)
        self.unit_path.unlink(missing_ok=True)
        self.config_path.unlink(missing_ok=True)
        self.host.reload()
        self.state_path.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('check', 'install', 'rollback'))
    parser.add_argument('interface', nargs='?')
    parser.add_argument('original_uuid', nargs='?')
    args = parser.parse_args()
    if args.action != 'rollback' and (not args.interface or not args.original_uuid):
        parser.error('check/install 需要显式指定用户网口和旧连接 UUID')
    if args.action != 'check' and os.geteuid() != 0:
        parser.error('请在终端使用 sudo 执行，不要发送密码到聊天')
    os.environ['PATH'] = '/usr/sbin:/usr/bin:/sbin:/bin'
    installer = Installer(Host())
    if args.action == 'check':
        installer.check(args.interface, args.original_uuid)
        print('预检通过，未修改网络。')
        return
    with open('/run/leros-wired-install.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.action == 'install':
            installer.install(args.interface, args.original_uuid)
            print('持久直连配置已安装；未接网线时等待自动连接，首次取址可能需要几十秒。')
        else:
            installer.rollback()
            print('已回退有线配置。旧配置可能与 Wi-Fi 冲突，请拔掉直连网线。')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(f'操作未完成：{error}', file=sys.stderr)
        sys.exit(1)
