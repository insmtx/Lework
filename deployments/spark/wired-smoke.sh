#!/usr/bin/env bash
# RJ45 直连最小验证；只临时替换指定网口的活动配置。
set -euo pipefail
export LC_ALL=C
unit=leros-wired-test.service
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fail() { echo "$*" >&2; exit 1; }
if [[ ${1:-check} != check && ${1:-check} != status ]]; then
  [[ $EUID == 0 ]] || fail '请使用 sudo 执行。'
fi
case ${1:-check} in
  status) systemctl --no-pager status "$unit"; exit ;;
  stop) systemctl stop "$unit"; exit ;;
  run|cleanup)
    [[ $script_dir =~ ^/run/leros-wired-test\.[a-zA-Z0-9]+$ ]] || fail '无效运行目录'
    source "$script_dir/config"
    if [[ $1 == cleanup ]]; then
      active_uuid=$(nmcli -g GENERAL.CON-UUID device show "$wired_interface" || true)
      if nmcli -g connection.uuid connection show uuid "$test_uuid" >/dev/null 2>&1; then
        nmcli --wait 15 connection delete uuid "$test_uuid"
      fi
      # 用户另行切换配置时不覆盖；只恢复本工具替换的原活动连接。
      if [[ $active_uuid == "$test_uuid" || $active_uuid == -- || -z $active_uuid ]]; then
        if [[ -n $original_uuid && $original_uuid != -- ]]; then
          nmcli --wait 15 connection up uuid "$original_uuid" ifname "$wired_interface"
        fi
      fi
      rm -f "$script_dir/config" "$script_dir/discovery.py" "$script_dir/wired-smoke.sh"
      rmdir "$script_dir" 2>/dev/null || true
      echo '临时有线配置已删除，原活动连接已按需恢复。'
      exit
    fi
    # 仅内存配置在测试期间优先重连；否则拔插后 NM 会激活旧静态配置。
    # 无 DHCP、DNS、默认网关和 NAT；不改变 Wi-Fi 或双机互联配置。
    nmcli connection add save no type ethernet ifname "$wired_interface" \
      con-name leros-wired-test connection.uuid "$test_uuid" connection.autoconnect yes \
      connection.autoconnect-priority 999 \
      ipv4.method link-local ipv4.never-default yes \
      ipv6.method link-local ipv6.never-default yes
    nmcli --wait 60 connection up uuid "$test_uuid" ifname "$wired_interface"
    ip -4 address show dev "$wired_interface" | grep -q 'inet 169\.254\.' || fail '未取得链路本地地址'
    python3 "$script_dir/discovery.py" --interface "$wired_interface" --port "$service_port" \
      --scheme "$service_scheme" --id-file /var/lib/leros-discovery/id &
    publisher_pid=$!
    echo '有线直连测试已启动，10 分钟后恢复原配置。'
    ip -brief -4 address show dev "$wired_interface"
    wait "$publisher_pid"
    fail '发现发布进程已退出'
    ;;
  check|start) ;;
  *) fail '用法：bash wired-smoke.sh check <网口>；sudo bash wired-smoke.sh start <网口>；sudo bash wired-smoke.sh stop' ;;
esac

wired_interface=${2:-}
[[ $wired_interface =~ ^[a-zA-Z0-9_.:-]+$ ]] || fail '请显式指定用户直连网口'
for command in nmcli ip python3 systemd-run; do
  command -v "$command" >/dev/null || fail "缺少依赖：$command"
done
[[ $(nmcli -g GENERAL.TYPE device show "$wired_interface") == ethernet ]] || fail '指定接口不是以太网'
[[ $(cat "/sys/class/net/$wired_interface/carrier") == 1 ]] || fail '网线未接通'
# 本次仅验证已核对的用户 RJ45 端口；避免误操作双机高速互联。
[[ $wired_interface == enP7s7 ]] || fail '本最小验证仅支持已确认的 enP7s7 用户网口'
ip -j -4 address show dev "$wired_interface" | python3 -c '
import ipaddress,json,sys
reserved=[ipaddress.ip_network(x) for x in ("10.100.216.0/24","10.100.217.0/24","10.42.0.0/16")]
for iface in json.load(sys.stdin):
    for addr in iface.get("addr_info",[]):
        if any(ipaddress.ip_address(addr["local"]) in net for net in reserved):
            sys.exit("拒绝修改集群或双机互联网口")
'
[[ -z $(systemctl list-units --all --plain --no-legend "$unit") ]] || fail '测试单元已存在'
python3 -c 'import dbus,uuid; uuid.UUID(open("/var/lib/leros-discovery/id").read().strip())'
original_uuid=$(nmcli -g GENERAL.CON-UUID device show "$wired_interface")
service_port=$(awk -F= '$1=="SERVICE_PORT" {print $2}' /etc/leros-discovery.conf)
service_scheme=$(awk -F= '$1=="SERVICE_SCHEME" {print $2}' /etc/leros-discovery.conf)
[[ $service_port =~ ^[0-9]{1,5}$ ]] && ((10#$service_port >= 1 && 10#$service_port <= 65535)) || fail '后端端口配置无效'
[[ $service_scheme == http || $service_scheme == https ]] || fail '后端协议配置无效'
[[ -f $script_dir/discovery.py ]] || fail '缺少同目录 discovery.py'
if [[ ${1:-check} == check ]]; then
  echo "预检通过：网口 $wired_interface，待恢复的原连接 UUID=$original_uuid。尚未修改网络。"
  exit
fi
test_uuid=$(python3 -c 'import uuid; print(uuid.uuid4())')
runtime_dir=$(mktemp -d /run/leros-wired-test.XXXXXXXX)
chmod 700 "$runtime_dir"
umask 077
install -m 700 "$script_dir/wired-smoke.sh" "$runtime_dir/wired-smoke.sh"
install -m 600 "$script_dir/discovery.py" "$runtime_dir/discovery.py"
declare -p wired_interface original_uuid test_uuid service_port service_scheme > "$runtime_dir/config"
if ! systemd-run --unit="$unit" --collect --no-block --property=Type=exec \
  --property=ExecStartPre='/bin/sleep 3' --property=RuntimeMaxSec=600 --property=TimeoutStopSec=45 \
  --property="ExecStopPost=/bin/bash $runtime_dir/wired-smoke.sh cleanup" \
  /bin/bash "$runtime_dir/wired-smoke.sh" run; then
  bash "$runtime_dir/wired-smoke.sh" cleanup
  exit 1
fi
echo '已提交 10 分钟有线测试，3 秒后开始；当前有线 SSH 可能断开。'
echo '笔记本有线接口改为 DHCP（自动取址），保持 Wi-Fi 原样。'
echo '查看日志：sudo journalctl -u leros-wired-test -n 30 --no-pager'
echo '提前恢复：sudo systemctl stop leros-wired-test'
