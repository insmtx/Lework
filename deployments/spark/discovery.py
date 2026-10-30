#!/usr/bin/env python3
"""Publish the configured Lework endpoint on one LAN interface via Avahi."""
import argparse
import signal
import socket
import threading
import uuid

import dbus

parser = argparse.ArgumentParser()
parser.add_argument('--interface', required=True)
parser.add_argument('--port', type=int, required=True)
parser.add_argument('--id-file', required=True)
parser.add_argument('--name', default='Lework Spark')
parser.add_argument('--scheme', choices=['http', 'https'], default='http')
args = parser.parse_args()
if not 1 <= args.port <= 65535:
    parser.error('invalid port')
with open(args.id_file, encoding='utf-8') as file:
    deployment_id = str(uuid.UUID(file.read().strip()))
stop = threading.Event()
for sig in (signal.SIGINT, signal.SIGTERM):
    signal.signal(sig, lambda *_: stop.set())
bus = dbus.SystemBus()
server = dbus.Interface(bus.get_object('org.freedesktop.Avahi', '/'), 'org.freedesktop.Avahi.Server')
group = dbus.Interface(bus.get_object('org.freedesktop.Avahi', server.EntryGroupNew()), 'org.freedesktop.Avahi.EntryGroup')
try:
    records = ['v=1', f'id={deployment_id}', f'scheme={args.scheme}', 'path=/v1']
    group.AddService(dbus.Int32(socket.if_nametoindex(args.interface)), dbus.Int32(0), dbus.UInt32(0),
                     args.name + ' - ' + deployment_id[-6:], '_lework._tcp', 'local', '', dbus.UInt16(args.port),
                     dbus.Array([dbus.ByteArray(item.encode()) for item in records], signature='ay'))
    group.Commit()
    print(f'Publishing {server.GetHostNameFqdn()}:{args.port} on {args.interface}', flush=True)
    while not stop.wait(5):
        # Avahi 重启、命名冲突或接口变化交由 systemd 重试，不永久保持失效状态。
        if int(group.GetState()) in (3, 4):
            raise RuntimeError('Avahi registration conflict or failure')
        socket.if_nametoindex(args.interface)
finally:
    try:
        group.Reset()
    except dbus.DBusException:
        pass
