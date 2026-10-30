#!/usr/bin/env python3
"""在 Spark 的指定接口临时广播，用于原型联调；退出后撤销。"""
import argparse
import signal
import socket
import threading
import uuid

import dbus

parser = argparse.ArgumentParser()
parser.add_argument('--interface', required=True)
parser.add_argument('--port', type=int, default=38081)
parser.add_argument('--duration', type=int, default=1800)
args = parser.parse_args()
if not 1 <= args.port <= 65535 or not 1 <= args.duration <= 3600:
    parser.error('端口需在 1–65535 范围，持续时间需在 1–3600 秒范围')
stop = threading.Event()
for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
    signal.signal(sig, lambda *_: stop.set())
bus = dbus.SystemBus()
server = dbus.Interface(bus.get_object('org.freedesktop.Avahi', '/'), 'org.freedesktop.Avahi.Server')
group = dbus.Interface(bus.get_object('org.freedesktop.Avahi', server.EntryGroupNew()), 'org.freedesktop.Avahi.EntryGroup')
records = ['v=1', 'id=prototype-' + str(uuid.uuid4()), 'scheme=http', 'path=/v1']
try:
    group.AddService(dbus.Int32(socket.if_nametoindex(args.interface)), dbus.Int32(0), dbus.UInt32(0),
                     'Lework Spark - Prototype', '_lework._tcp', 'local', '', dbus.UInt16(args.port),
                     dbus.Array([dbus.ByteArray(item.encode()) for item in records], signature='ay'))
    group.Commit()
    print(f'广播已启动：{server.GetHostNameFqdn()}:{args.port}，{args.duration} 秒后撤销', flush=True)
    stop.wait(args.duration)
finally:
    group.Reset()
    print('临时广播已撤销', flush=True)
