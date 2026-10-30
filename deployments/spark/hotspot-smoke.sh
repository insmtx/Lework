#!/usr/bin/env bash
# 临时验证同网卡 STA + AP；不作为生产环境热点管理器。
set -euo pipefail
export LC_ALL=C
unit=leros-hotspot-test.service
ap_interface=lw-ap0
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fail() { echo "$*" >&2; exit 1; }
if [[ ${1:-start} != check && ${1:-start} != status ]]; then
  [[ $EUID == 0 ]] || fail '请在 Spark 上用 sudo 执行。'
fi

case ${1:-start} in
  stop) systemctl stop "$unit"; exit ;;
  status) systemctl --no-pager status "$unit"; exit ;;
  run|cleanup)
    # 后台进程只读取 start 阶段复制到 root 私有目录的配置。
    [[ $script_dir =~ ^/run/leros-hotspot-test\.[a-zA-Z0-9]+$ ]] || fail '无效的运行目录'
    source "$script_dir/config"
    if [[ $1 == cleanup ]]; then
      if nmcli -g connection.uuid connection show uuid "$profile_uuid" >/dev/null 2>&1; then
        nmcli --wait 15 connection delete uuid "$profile_uuid" || true
      fi
      if [[ -f $script_dir/interface-created ]]; then
        iw dev "$ap_interface" del 2>/dev/null || true
      fi
      rm -f "$script_dir/psk" "$script_dir/config" "$script_dir/interface-created" \
        "$script_dir/discovery.py" "$script_dir/hotspot-smoke.sh"
      rmdir "$script_dir" 2>/dev/null || true
      echo '测试热点已撤销；未改动原 Wi-Fi 连接配置。'
      exit
    fi
    [[ ! -e /sys/class/net/$ap_interface ]] || fail '测试网卡名已被占用'
    touch "$script_dir/interface-created"
    iw dev "$upstream" interface add "$ap_interface" type __ap
    # 不创建落盘、可自动连接的热点配置；密码不写入日志。
    nmcli connection add save no type wifi ifname "$ap_interface" con-name leros-hotspot-test \
      connection.uuid "$profile_uuid" connection.autoconnect no \
      802-11-wireless.ssid "$ssid" 802-11-wireless.mode ap \
      802-11-wireless.band "$band" 802-11-wireless.channel "$channel" \
      802-11-wireless-security.key-mgmt wpa-psk \
      802-11-wireless-security.proto rsn \
      802-11-wireless-security.psk "$(cat "$script_dir/psk")" \
      ipv4.method shared ipv4.addresses 192.168.78.1/24 \
      ipv4.never-default yes ipv6.method disabled
    nmcli --wait 30 connection up uuid "$profile_uuid" ifname "$ap_interface"
    [[ $(nmcli -g GENERAL.STATE device show "$upstream") == '100 (connected)' ]] || fail '上游连接丢失，撤销热点'
    python3 "$script_dir/discovery.py" --interface "$ap_interface" --port "$service_port" \
      --scheme "$service_scheme" --id-file /var/lib/leros-discovery/id &
    publisher_pid=$!
    echo "热点已启动：$ssid；网关 192.168.78.1；上游 $upstream；10 分钟后自动关闭。"
    # 上游丢失或改变信道时结束测试，不自动重配上游 Wi-Fi。
    while kill -0 "$publisher_pid" 2>/dev/null; do
      sleep 5
      current_channel=$(iw dev "$upstream" info | awk '/channel / {print $2; exit}')
      [[ $current_channel == "$channel" ]] || fail '上游信道改变，结束测试'
      [[ $(nmcli -g GENERAL.STATE device show "$upstream") == '100 (connected)' ]] || fail '上游断开，结束测试'
    done
    fail '发现发布进程已退出'
    ;;
  start|check) ;;
  *) fail '用法：bash hotspot-smoke.sh check；sudo bash hotspot-smoke.sh [start <上游无线网卡>|status|stop]' ;;
esac

upstream=${2:-wlP9s9}
[[ $upstream =~ ^[a-zA-Z0-9_.:-]+$ ]] || fail '无效的上游网卡名'
for command in nmcli iw ip python3 systemd-run dnsmasq; do
  command -v "$command" >/dev/null || fail "缺少依赖：$command"
done
[[ -z $(systemctl list-units --all --plain --no-legend "$unit") ]] || fail '测试单元已存在，请先查看 status / stop'
[[ ! -e /sys/class/net/$ap_interface ]] || fail '测试网卡名已被占用'
[[ $(nmcli -g GENERAL.STATE device show "$upstream") == '100 (connected)' ]] || fail '上游 Wi-Fi 未连接'
[[ $(nmcli -g GENERAL.TYPE device show "$upstream") == wifi ]] || fail '上游不是无线网卡'
[[ $(nmcli -g WIFI-PROPERTIES.AP device show "$upstream") == yes ]] || fail '网卡不支持热点'
ip -4 route show default | grep -q " dev $upstream " || fail '当前默认上网路由不是指定无线网卡'
channel=$(iw dev "$upstream" info | awk '/channel / {print $2; exit}')
frequency=$(iw dev "$upstream" info | awk '/channel / {gsub(/[()]/,"",$3); print $3; exit}')
[[ $channel =~ ^[0-9]+$ && $frequency =~ ^[0-9]+$ ]] || fail '无法读取上游信道'
if ((frequency < 2500)); then band=bg
elif ((frequency >= 5000 && frequency < 5900)); then band=a
else fail '本次验证仅支持 2.4/5 GHz 上游连接'; fi
phy=$(iw dev "$upstream" info | awk '/wiphy / {print "phy" $2; exit}')
channel_info=$(iw phy "$phy" channels | awk -v f="$frequency" '/\* .* MHz/ {matched=($2==f)} matched {print}')
[[ -n $channel_info ]] || fail '无法确认信道限制'
if grep -Eqi 'disabled|no IR|radar|DFS' <<< "$channel_info"; then
  fail '当前信道不适合此最小热点验证，请先由运维选择非 DFS 上游网络'
fi
ip -j -4 route show table all | python3 -c '
import ipaddress,json,sys
candidate=ipaddress.ip_network("192.168.78.0/24")
for row in json.load(sys.stdin):
    dest=row.get("dst","default")
    if dest=="default": continue
    if candidate.overlaps(ipaddress.ip_network(dest,strict=False)):
        sys.exit("测试热点网段与现有路由冲突，未修改网络")
'
python3 -c 'import dbus,uuid; uuid.UUID(open("/var/lib/leros-discovery/id").read().strip())'
service_port=$(awk -F= '$1=="SERVICE_PORT" {print $2}' /etc/leros-discovery.conf)
service_scheme=$(awk -F= '$1=="SERVICE_SCHEME" {print $2}' /etc/leros-discovery.conf)
[[ $service_port =~ ^[0-9]{1,5}$ ]] && ((10#$service_port >= 1 && 10#$service_port <= 65535)) || fail '后端端口配置无效'
[[ $service_scheme == http || $service_scheme == https ]] || fail '后端协议配置无效'
[[ -f $script_dir/discovery.py ]] || fail '请将 discovery.py 与本脚本放在同一目录'
if [[ ${1:-start} == check ]]; then
  echo "预检通过：上游 $upstream，信道 $channel / $band，测试网段 192.168.78.0/24，后端 $service_scheme:$service_port。"
  echo '尚未创建热点；驱动并发、客户端上网和自动清理仍须实机验证。'
  exit
fi
profile_uuid=$(python3 -c 'import uuid; print(uuid.uuid4())')
ssid="Lework-Spark-$(tail -c 7 /var/lib/leros-discovery/id | tr -d '\n')"
runtime_dir=$(mktemp -d /run/leros-hotspot-test.XXXXXXXX)
chmod 700 "$runtime_dir"
umask 077
install -m 700 "$script_dir/hotspot-smoke.sh" "$runtime_dir/hotspot-smoke.sh"
install -m 600 "$script_dir/discovery.py" "$runtime_dir/discovery.py"
declare -p upstream channel band profile_uuid ssid service_port service_scheme > "$runtime_dir/config"
python3 -c 'import secrets; print(secrets.token_hex(8))' > "$runtime_dir/psk"
if ! systemd-run --unit="$unit" --collect --property=Type=exec \
  --property=RuntimeMaxSec=600 --property=TimeoutStopSec=30 \
  --property="ExecStopPost=/bin/bash $runtime_dir/hotspot-smoke.sh cleanup" \
  /bin/bash "$runtime_dir/hotspot-smoke.sh" run; then
  bash "$runtime_dir/hotspot-smoke.sh" cleanup
  exit 1
fi
echo "已提交 10 分钟热点测试。请先确认 journal 日志出现‘热点已启动’再连接。"
echo "Wi-Fi 名称：$ssid"
printf '临时 Wi-Fi 密码（仅此终端显示）：'; cat "$runtime_dir/psk"
echo '查看日志：sudo journalctl -u leros-hotspot-test -n 30 --no-pager'
echo '提前关闭：sudo systemctl stop leros-hotspot-test'
