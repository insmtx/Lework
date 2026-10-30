#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ ${EUID} -ne 0 ]]; then echo '请由运维使用 sudo 执行。' >&2; exit 1; fi
if [[ ${1:-} == --uninstall ]]; then
  systemctl disable --now leros-discovery.service
  rm -f /etc/systemd/system/leros-discovery.service
  systemctl daemon-reload
  echo '已停止并卸载服务单元；保留配置和设备 ID 供重新安装。'
  exit 0
fi
lan_interface=${1:-}
service_port=${2:-}
service_scheme=${3:-http}
[[ $lan_interface =~ ^[a-zA-Z0-9_.:-]+$ ]] || { echo '用法：sudo bash install-discovery.sh <网卡> <端口> [http|https]'; exit 1; }
[[ $service_port =~ ^[0-9]{1,5}$ ]] && ((10#$service_port >= 1 && 10#$service_port <= 65535)) || exit 1
[[ $service_scheme == http || $service_scheme == https ]] || exit 1
python3 -c 'import dbus, socket, sys; socket.if_nametoindex(sys.argv[1])' "$lan_interface"
id avahi >/dev/null
systemctl is-active --quiet avahi-daemon
install -d -m 755 /usr/local/lib/leros-discovery /var/lib/leros-discovery
for file in /etc/leros-discovery.conf /etc/systemd/system/leros-discovery.service /usr/local/lib/leros-discovery/discovery.py; do
  if [[ -f $file ]]; then cp -p "$file" "$file.bak.$(date +%s)"; fi
done
if [[ ! -e /var/lib/leros-discovery/id ]]; then
  python3 -c 'import uuid; print(uuid.uuid4())' > /var/lib/leros-discovery/id
fi
chmod 644 /var/lib/leros-discovery/id
printf 'LAN_INTERFACE=%s\nSERVICE_PORT=%s\nSERVICE_SCHEME=%s\n' "$lan_interface" "$service_port" "$service_scheme" > /etc/leros-discovery.conf
chmod 644 /etc/leros-discovery.conf
install -m 644 "$script_dir/discovery.py" /usr/local/lib/leros-discovery/discovery.py
install -m 644 "$script_dir/leros-discovery.service" /etc/systemd/system/leros-discovery.service
systemctl daemon-reload
systemctl enable leros-discovery.service
systemctl restart leros-discovery.service
systemctl --no-pager status leros-discovery.service
