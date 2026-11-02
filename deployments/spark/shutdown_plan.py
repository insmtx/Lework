#!/usr/bin/env python3
"""Validate an offline, single-host shutdown draft; never execute service or power actions."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys
import time

MAX_BYTES = 1024 * 1024
MAX_TARGETS = 256
STRATEGIES = {'docker': 'docker_graceful', 'kubernetes': 'kubernetes_scaledown'}
STATES = {'docker': {'running': True, 'exited': False, 'created': False},
          'kubernetes': {'Running': True, 'Succeeded': False}}
UNIMPLEMENTED = ['execution_unimplemented', 'request_drain_unverified',
                 'controller_stop_unverified', 'recovery_unverified',
                 'system_shutdown_unverified', 'independent_shutdown_confirmation_unavailable']


def text(value):
    return (isinstance(value, str) and 0 < len(value) <= 512
            and all(ord(c) >= 32 and not 0xD800 <= ord(c) <= 0xDFFF for c in value))


def validate_registry(registry):
    if (not isinstance(registry, dict) or set(registry) != {'version', 'host_id', 'targets'}
            or type(registry['version']) is not int or registry['version'] != 1
            or not text(registry['host_id']) or not isinstance(registry['targets'], list)
            or len(registry['targets']) > MAX_TARGETS):
        raise ValueError('invalid registry')
    targets = {}
    bindings = set()
    for target in registry['targets']:
        if (not isinstance(target, dict) or set(target) != {
                'id', 'kind', 'component_id', 'scope', 'depends_on', 'stop_strategy', 'restore_strategy'}
                or not isinstance(target['id'], str)
                or not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,63}', target['id'])
                or not isinstance(target['kind'], str) or target['kind'] not in STRATEGIES
                or not text(target['component_id']) or target['scope'] != 'local'
                or target['stop_strategy'] != STRATEGIES[target['kind']]
                or target['restore_strategy'] != 'previous_state'
                or not isinstance(target['depends_on'], list)
                or len(target['depends_on']) > MAX_TARGETS
                or not all(isinstance(d, str) for d in target['depends_on'])):
            raise ValueError('invalid target')
        deps = target['depends_on']
        binding = (target['kind'], target['component_id'])
        if (target['id'] in targets or binding in bindings
                or len(set(deps)) != len(deps) or target['id'] in deps):
            raise ValueError('duplicate target or invalid dependency')
        targets[target['id']] = dict(target, depends_on=sorted(deps))
        bindings.add(binding)
    if any(d not in targets for target in targets.values() for d in target['depends_on']):
        raise ValueError('missing dependency')
    return {key: targets[key] for key in sorted(targets)}


def build_plan(registry, snapshot, now):
    """Return a diagnostic draft, never an execution authorization or restore command."""
    targets = validate_registry(registry)
    problems = set()
    result = {'version': 1, 'host_id': registry['host_id'], 'boot_id': None,
              'observed_at': None, 'policy_valid': False, 'executable': False,
              'stop_order': [], 'restore_order': [], 'keep_stopped': [],
              'unregistered': [], 'missing_targets': [], 'plan_digest': None}
    inventory = {}
    if not isinstance(snapshot, dict):
        snapshot = {}
    stamp = snapshot.get('observed_at')
    if (type(snapshot.get('protocol_version')) is not int or snapshot.get('protocol_version') != 1
            or snapshot.get('host_id') != registry['host_id'] or not text(snapshot.get('boot_id'))
            or type(stamp) is not int or not 0 < stamp <= 9007199254740991
            or not -5 <= now - stamp <= 30):
        problems.add('snapshot_invalid_or_stale')
    else:
        result.update(boot_id=snapshot['boot_id'], observed_at=stamp)
    if snapshot.get('inventory_complete') is not True:
        problems.add('inventory_incomplete')
    components = snapshot.get('components')
    if not isinstance(components, list) or len(components) > MAX_TARGETS:
        problems.add('inventory_invalid')
        components = []
    for c in components:
        if (not isinstance(c, dict) or not isinstance(c.get('kind'), str)
                or c['kind'] not in STATES or not text(c.get('id')) or not text(c.get('state'))):
            problems.add('inventory_invalid')
            continue
        key = (c['kind'], c['id'])
        if key in inventory:
            problems.add('inventory_invalid')
        inventory[key] = {'kind': c['kind'], 'id': c['id'], 'state': c['state']}
        if c['state'] not in STATES[c['kind']]:
            problems.add('workload_state_unknown')
    bound = {(t['kind'], t['component_id']): key for key, t in targets.items()}
    result['unregistered'] = [inventory[key] for key in sorted(inventory.keys() - bound.keys())]
    result['missing_targets'] = sorted(bound[key] for key in bound.keys() - inventory.keys())
    if result['unregistered']:
        problems.add('unregistered_workloads')
    if result['missing_targets']:
        problems.add('registered_target_missing')

    # 先计算依赖先行顺序，再反转为停止顺序；不根据服务名称猜测依赖。
    remaining = set(targets)
    ordered = []
    while remaining:
        ready = sorted(key for key in remaining if not (set(targets[key]['depends_on']) & remaining))
        if not ready:
            problems.add('dependency_cycle')
            break
        ordered.extend(ready)
        remaining.difference_update(ready)
    running = {}
    for key, target in targets.items():
        c = inventory.get((target['kind'], target['component_id']))
        if c is not None and c['state'] in STATES[c['kind']]:
            running[key] = STATES[c['kind']][c['state']]
    for key, target in targets.items():
        if running.get(key) and any(running.get(d) is False for d in target['depends_on']):
            problems.add('dependency_not_running')
    if not problems:
        result['policy_valid'] = True
        result['stop_order'] = [key for key in reversed(ordered) if running[key]]
        result['restore_order'] = [key for key in ordered if running[key]]
        result['keep_stopped'] = sorted(key for key in targets if not running[key])
        # 摘要仅用于比较候选内容，不是预检票据；真正执行必须重新采集并确认目标身份。
        canonical = {'host_id': result['host_id'], 'boot_id': result['boot_id'],
                     'observed_at': stamp, 'targets': targets,
                     'inventory': [inventory[key] for key in sorted(inventory)]}
        result['plan_digest'] = hashlib.sha256(json.dumps(
            canonical, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    result['blockers'] = sorted(problems) + UNIMPLEMENTED
    return result


def read_json(path):
    def unique(pairs):
        obj = {}
        for key, value in pairs:
            if key in obj:
                raise ValueError('duplicate JSON field')
            obj[key] = value
        return obj
    with Path(path).open('rb') as stream:
        data = stream.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ValueError('input too large')
    return json.loads(data, object_pairs_hook=unique)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--registry', required=True, help='candidate registration JSON; never authorizes execution')
    parser.add_argument('--snapshot', required=True, help='fresh power_agent --snapshot JSON from the same host')
    args = parser.parse_args(argv)
    try:
        result = build_plan(read_json(args.registry), read_json(args.snapshot), time.time())
    except (OSError, ValueError, TypeError, RecursionError):
        print(json.dumps({'error': 'invalid_plan_input', 'executable': False}))
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 1  # 仅预检；即使清单有效也存在未实现的执行条件。


if __name__ == '__main__':
    sys.exit(main())
